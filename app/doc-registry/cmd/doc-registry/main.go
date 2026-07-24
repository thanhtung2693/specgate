package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/api"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/config"
	"github.com/specgate/doc-registry/internal/governancefiles"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/integrations"
	"github.com/specgate/doc-registry/internal/knowledge"
	"github.com/specgate/doc-registry/internal/knowledgequeue"
	"github.com/specgate/doc-registry/internal/observability"
	"github.com/specgate/doc-registry/internal/retention"
	"github.com/specgate/doc-registry/internal/seeding"
	"github.com/specgate/doc-registry/internal/settings"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/storage/blob"
	storagedb "github.com/specgate/doc-registry/internal/storage/db"
	"github.com/specgate/doc-registry/internal/storage/localobject"
	"github.com/specgate/doc-registry/internal/storage/pgvector"
	"github.com/specgate/doc-registry/internal/storage/s3"
	"github.com/specgate/doc-registry/internal/webhookqueue"
)

// agentsRunnerOrNil keeps a nil *agentsclient.Client from becoming a non-nil
// AgentsRunner interface (typed-nil hazard).
func agentsRunnerOrNil(c *agentsclient.Client) governanceops.AgentsRunner {
	if c == nil {
		return nil
	}
	return c
}

func knowledgeObjectStoreFor(storageDriver string, s3Client *s3.Client) knowledge.ObjectStore {
	if storageDriver == "s3" && s3Client != nil {
		return s3Client
	}
	return knowledge.NullObjectStore{}
}

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply migrations then exit")
	seedSkillsFlag := flag.Bool("seed-skills", false, "register missing gate-rubric skills, then exit")
	seedSkillsOverwriteFlag := flag.Bool("seed-skills-overwrite", false, "with --seed-skills, update existing starter skills by stable name")
	seedDemoFlag := flag.Bool("seed-demo", false, "create local demo governance data for UI development, then exit")
	seedDemoWorkspaceID := flag.String("seed-demo-workspace-id", "", "assign seeded demo work items to this workspace ID")
	seedDemoCreatedBy := flag.String("seed-demo-created-by", "", "record this actor as the creator of seeded demo work items")
	flag.Parse()

	_ = godotenv.Load()

	log := zerolog.New(os.Stdout).With().Timestamp().Logger()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	if cfg.SettingsEncryptionKey == "" {
		log.Fatal().Msg("SETTINGS_ENCRYPTION_KEY is required (32-byte hex, e.g. openssl rand -hex 32)")
	}

	sentryCleanup, err := observability.InitSentry(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("init sentry")
	}
	defer sentryCleanup()

	gormDB, err := storagedb.Open(cfg.PostgresDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("open database")
	}
	defer func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	if err := storagedb.Migrate(gormDB); err != nil {
		log.Fatal().Err(err).Msg("migrate")
	}
	schemaStatus, err := storagedb.RequiredSchemaStatus(context.Background(), gormDB)
	if err != nil {
		log.Fatal().Err(err).Msg("check database schema")
	}
	if schemaStatus.Status != "ok" {
		log.Fatal().Strs("missing", schemaStatus.Missing).Msg(
			"incompatible database schema: this development build requires a fresh database",
		)
	}
	if *migrateOnly {
		log.Info().Msg("migrations applied; exiting (--migrate-only)")
		return
	}
	identityRepo := storagedb.NewIdentityRepository(gormDB)

	settingsCrypto, err := settings.NewCrypto(cfg.SettingsEncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("init settings crypto")
	}
	settingsRepo := storagedb.NewSettingsRepository(gormDB)
	settingsSvc, err := settings.NewService(settingsRepo, settingsCrypto)
	if err != nil {
		log.Warn().Err(err).Msg("settings initial load (using defaults)")
	}
	defer settingsSvc.Stop()

	skillsSvc := skills.NewService(storagedb.NewSkillRepository(gormDB))
	integrationsRepo := storagedb.NewIntegrationRepository(gormDB)

	seedSkillsForWorkspaces := func(opts seeding.SkillSeedOptions) (seeding.Result, error) {
		workspaces, err := identityRepo.ListWorkspaces(context.Background())
		if err != nil || len(workspaces) == 0 {
			return seeding.Result{}, err
		}
		var total seeding.Result
		for _, workspace := range workspaces {
			result, seedErr := seeding.SeedSkillsWithOptions(skills.WithWorkspace(context.Background(), workspace.ID), skillsSvc, settingsSvc, &log, opts)
			if seedErr != nil {
				return total, seedErr
			}
			total.SkillsCreated = append(total.SkillsCreated, result.SkillsCreated...)
			total.SkillsUpdated = append(total.SkillsUpdated, result.SkillsUpdated...)
			total.SkillsExisting = append(total.SkillsExisting, result.SkillsExisting...)
		}
		return total, nil
	}

	if *seedSkillsFlag {
		result, err := seedSkillsForWorkspaces(seeding.SkillSeedOptions{
			OverwriteExisting: *seedSkillsOverwriteFlag,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("seed-skills failed")
		}
		log.Info().
			Int("skills_created", len(result.SkillsCreated)).
			Int("skills_updated", len(result.SkillsUpdated)).
			Int("skills_existing", len(result.SkillsExisting)).
			Msg("seed-skills complete")
		return
	}

	// Auto-seed gate-rubric skills on every startup so quality gates work out of
	// the box without a separate make seed-skills step. SeedSkills is idempotent —
	// existing skills that already match the seed are left untouched, so this is
	// effectively free after the first boot.
	if result, err := seedSkillsForWorkspaces(seeding.SkillSeedOptions{}); err != nil {
		log.Warn().Err(err).Msg("auto-seed gate-rubric skills failed — quality gates may be missing rubrics")
	} else if len(result.SkillsCreated) > 0 || len(result.SkillsUpdated) > 0 {
		log.Info().
			Int("created", len(result.SkillsCreated)).
			Int("updated", len(result.SkillsUpdated)).
			Strs("new", result.SkillsCreated).
			Msg("gate-rubric skills synced on startup")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var blobStore blob.Store
	var s3Client *s3.Client
	var artifactObjectStore artifact.ObjectStore
	var governanceFileObjectDeleter governancefiles.ObjectDeleter

	switch cfg.Blob.Driver {
	case "s3":
		s3Client, err = s3.New(ctx, cfg.S3)
		if err != nil {
			log.Fatal().Err(err).Msg("init s3 client")
		}
		artifactObjectStore = s3Client
		governanceFileObjectDeleter = s3Client
		log.Info().Str("endpoint", cfg.S3.Endpoint).Msg("storage driver: s3")
	default: // "local"
		blobStore, err = blob.NewLocalStore(cfg.Blob.DataRoot)
		if err != nil {
			log.Fatal().Err(err).Str("data_root", cfg.Blob.DataRoot).Msg("init local blob store")
		}
		artifactObjectStore, err = localobject.NewStore(cfg.Blob.DataRoot + "/artifacts")
		if err != nil {
			log.Fatal().Err(err).Str("data_root", cfg.Blob.DataRoot+"/artifacts").Msg("init local artifact object store")
		}
		log.Info().Str("data_root", cfg.Blob.DataRoot).Msg("storage driver: local")
		if cfg.S3.Endpoint != "" {
			s3Client, err = s3.New(ctx, cfg.S3)
			if err != nil {
				log.Warn().Err(err).Msg("s3 init skipped (optional for local driver)")
			}
		}
		if s3Client != nil {
			governanceFileObjectDeleter = governancefiles.NewRoutedObjectDeleter(blobStore, s3Client)
		} else {
			governanceFileObjectDeleter = governancefiles.NewBlobObjectDeleter(blobStore)
		}
	}
	// blobStore is wired into governance file upload/serve and knowledge upload paths.

	repo := storagedb.NewRepository(gormDB)
	workBoardRepo := storagedb.NewWorkBoardRepository(gormDB)
	workBoardRepo.SetDeliverySLADays(cfg.DeliverySLADays)
	workBoardRepo.SetAutoArchiveOnDeliveryPass(func() bool {
		return settingsSvc.GetBool(settings.KeyGovernanceAutoArchiveOnDeliveryPass)
	})
	// Async inbound-webhook queue. Driver is controlled by QUEUE_DRIVER:
	//   "sync"  (default) — webhooks process inline; no Redis required.
	//   "redis" — asynq async queue over Redis (requires REDIS_URL).
	// webhookEnqueuer stays a nil interface when disabled — a nil *Client would be
	// a non-nil interface holding a nil pointer, which the Service would treat as
	// "enabled" and panic on use.
	var webhookEnqueuer webhookqueue.Enqueuer
	var webhookClient *webhookqueue.Client
	var webhookWorker *asynq.Server
	// knowledgeEnqueuer stays a nil interface under sync (never a typed nil
	// pointer) so the knowledge service branches to inline ingestion.
	var knowledgeEnqueuer knowledgequeue.Enqueuer
	var knowledgeClient *knowledgequeue.Client
	switch cfg.Queue.Driver {
	case "redis":
		if cfg.Redis.URL == "" {
			log.Fatal().Msg("QUEUE_DRIVER=redis requires REDIS_URL to be set")
		}
		redisOpt, err := asynq.ParseRedisURI(cfg.Redis.URL)
		if err != nil {
			log.Fatal().Err(err).Msg("parse REDIS_URL for async queue")
		}
		webhookClient = webhookqueue.NewClient(redisOpt, cfg.WebhookQueue.MaxRetry)
		webhookEnqueuer = webhookClient
		knowledgeClient = knowledgequeue.NewClient(redisOpt, cfg.WebhookQueue.MaxRetry)
		knowledgeEnqueuer = knowledgeClient
		webhookWorker = asynq.NewServer(redisOpt, asynq.Config{
			Concurrency: cfg.WebhookQueue.Concurrency,
			Queues:      map[string]int{webhookqueue.QueueName: 1, knowledgequeue.QueueName: 1},
		})
	default: // "sync"
		log.Info().Msg("queue driver: sync (inline webhook + knowledge ingest processing, no Redis required)")
	}

	integrationsSvc := integrations.NewServiceWithWorkBoard(integrationsRepo, workBoardRepo).WithOAuthAppLookup(func(_ context.Context, provider string, hostKey string) (*integrations.OAuthAppConfig, error) {
		var creds config.OAuthAppCredentials
		var configuredHostKey string
		switch provider {
		case integrations.ProviderGitHub:
			creds = cfg.OAuth.GitHub
			configuredHostKey = "github.github_com"
		case integrations.ProviderGitLab:
			creds = cfg.OAuth.GitLab
			configuredHostKey = "gitlab.gitlab_com"
		case integrations.ProviderLinear:
			creds = cfg.OAuth.Linear
			configuredHostKey = "linear.linear_app"
		}
		if creds.ClientID == "" || creds.ClientSecret == "" || hostKey != configuredHostKey {
			// Not configured — the OAuth flow treats a nil app as a validation error.
			return nil, nil
		}
		return &integrations.OAuthAppConfig{
			Provider:     provider,
			HostKey:      configuredHostKey,
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
		}, nil
	}).WithWebhookEnqueuer(webhookEnqueuer)

	// The async worker is started after the knowledge service is built (below) so
	// both the webhook and knowledge-ingest handlers register on one mux.
	governanceFilesRepo := storagedb.NewGovernanceFilesRepository(gormDB)
	artifactAttachmentsRepo := storagedb.NewArtifactAttachmentRepository(gormDB)
	knowledgeRepo := storagedb.NewKnowledgeRepository(gormDB)
	artifactObjectKey := func(artifactID, version, filename string) string {
		return s3.ObjectKey(cfg.S3.KeyPrefix, artifactID, version, filename)
	}
	artifactSvc := artifact.NewService(repo, artifactObjectStore, artifactObjectKey)
	if *seedDemoFlag {
		if strings.TrimSpace(*seedDemoWorkspaceID) == "" {
			log.Fatal().Msg("seed-demo requires --seed-demo-workspace-id")
		}
		result, err := seeding.SeedDemo(context.Background(), seeding.DemoDeps{
			WorkBoard:    workBoardRepo,
			Artifacts:    artifactSvc,
			Integrations: integrationsRepo,
			Knowledge:    knowledgeRepo,
			Logger:       &log,
			WorkspaceID:  *seedDemoWorkspaceID,
			CreatedBy:    *seedDemoCreatedBy,
		})
		if err != nil {
			log.Fatal().Err(err).Msg("seed-demo failed")
		}
		log.Info().
			Int("features_created", result.FeaturesCreated).
			Int("features_existing", result.FeaturesExisting).
			Int("change_requests_created", result.ChangeRequestsCreated).
			Int("artifacts_published", result.ArtifactsPublished).
			Int("knowledge_created", result.KnowledgeCreated).
			Int("feedback_created", result.FeedbackCreated).
			Msg("seed-demo complete")
		return
	}
	// Select the vector store backend via KNOWLEDGE_DRIVER.
	var vectorStore knowledge.VectorStore
	switch cfg.Knowledge.Driver {
	case "pgvector":
		pvStore, err := pgvector.New(ctx, cfg.PostgresDSN, cfg.Knowledge.EmbeddingDim)
		if err != nil {
			log.Fatal().Err(err).Msg("pgvector: connect")
		}
		if err := pvStore.EnsureCollection(ctx); err != nil {
			log.Fatal().Err(err).Msg("pgvector: ensure collection")
		}
		defer pvStore.Close() // release the pgvector pool on shutdown
		vectorStore = pvStore
	default: // "none"
		log.Info().Msg("knowledge driver: none (vector search disabled)")
		vectorStore = knowledge.NullVectorStore{}
	}
	// Embeddings are configured in Settings → Models (provider + model + that
	// provider's api_key) and resolved at call time, so a change takes effect
	// without a restart. With nothing configured the server boots with knowledge
	// search/upload soft-disabled and the UI warns + disables upload.
	embedder := knowledge.NewSettingsEmbedder(func() knowledge.EmbeddingConfig {
		provider := settingsSvc.Get(settings.KeyEmbeddingModelProvider)
		return knowledge.EmbeddingConfig{
			Provider: provider,
			Model:    settingsSvc.Get(settings.KeyEmbeddingModel),
			APIKey:   embeddingAPIKey(settingsSvc, provider),
		}
	}, cfg.Knowledge.EmbeddingDim)
	if !embedder.Enabled() {
		log.Warn().Msg("knowledge embeddings disabled — set an Embedding Model + provider key in Settings → Models to enable knowledge search/upload")
	}
	// Knowledge source bytes go to S3 when configured; local mode uses
	// NullObjectStore (vector chunks in pgvector are the source of truth for search).
	knowledgeObjectStore := knowledgeObjectStoreFor(cfg.Blob.Driver, s3Client)
	knowledgeSvc, err := knowledge.NewService(
		knowledgeRepo,
		knowledgeObjectStore,
		vectorStore,
		embedder,
		cfg.Knowledge.MaxFileBytes,
		cfg.S3.KeyPrefix,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("init knowledge service")
	}
	// Async ingest: enqueuer stays a nil interface under sync (inline ingest).
	knowledgeSvc = knowledgeSvc.WithIngestEnqueuer(knowledgeEnqueuer)

	// Start the async worker when QUEUE_DRIVER=redis. One mux dispatches enqueued
	// webhook deliveries and knowledge ingestions back through their services.
	if webhookWorker != nil {
		mux := asynq.NewServeMux()
		mux.HandleFunc(webhookqueue.TaskTypeWebhookDeliver, webhookqueue.Handler(integrationsSvc))
		mux.HandleFunc(knowledgequeue.TaskTypeKnowledgeIngest, knowledgequeue.Handler(knowledgeSvc))
		if err := webhookWorker.Start(mux); err != nil {
			log.Fatal().Err(err).Msg("start async queue worker")
		}
		log.Info().Int("concurrency", cfg.WebhookQueue.Concurrency).Msg("async queue: worker started (webhooks + knowledge ingest)")
	}

	agentsClient := agentsclient.New(cfg.AgentsBaseURL)
	var governanceChatHealth func(context.Context) (bool, error)
	if agentsClient != nil {
		governanceChatHealth = func(ctx context.Context) (bool, error) {
			health, err := agentsClient.ChatHealth(ctx)
			return health.Configured, err
		}
	}

	govSvc := &governanceops.Service{
		WorkBoard:       workBoardRepo,
		WorkItems:       workBoardRepo,
		Trackers:        integrationsSvc,
		AppBaseURL:      cfg.PublicAppBaseURL,
		Artifacts:       artifactSvc,
		ReadinessRuns:   artifactSvc, // surfaces spec_repo_drift readiness runs in the pack
		AuditEvents:     artifactSvc, // artifact status events for the audit trail
		AuditLifecycle:  workBoardRepo,
		Attachments:     artifactAttachmentsRepo,
		Skills:          skillsSvc,
		Knowledge:       knowledgeRepo,
		FeedbackStore:   integrationsSvc,
		ArtifactWriter:  artifactSvc,
		FeatureUpserter: workBoardRepo,
		ProfileResolver: governanceprofile.Resolver{},
		// agentsclient.New returns a nil *Client when AGENTS_BASE_URL is empty; a
		// typed-nil stored in the interface would read as configured. Guard it.
		AgentsRunner: agentsRunnerOrNil(agentsClient),
		StatsSource:  workBoardRepo,
	}

	// Shared by HTTP handlers so all governance operations read/write the same tasks.
	// storagedb.Open is fatal above, so gormDB is always non-nil here.
	gateTaskStore := storagedb.NewPGGateTaskStore(gormDB)

	retentionSweeper := &retention.Sweeper{
		Candidates: repo,
		Referenced: workBoardRepo,
		Artifacts:  artifactSvc,
		GateRows:   repo,
		Interval:   cfg.ArtifactRetentionSweepIntervalDur,
		// Read the toggle from settings on every tick so a Settings UI change
		// takes effect without restart; the env flag keeps it on regardless.
		Enabled: func() bool {
			return cfg.ArtifactRetentionSweepEnabled || settingsSvc.GetBool(settings.KeyArtifactRetentionSweepEnabled)
		},
	}
	maintenanceCleanup := func(ctx context.Context, workspaceID string) (api.MaintenanceCleanupCounts, error) {
		ctx = artifact.WithWorkspace(ctx, workspaceID)
		// Purge and demo removal run before the sweep: they un-reference
		// expired artifacts, so one cleanup pass leaves nothing behind.
		archived, err := workBoardRepo.PurgeArchivedChangeRequests(ctx)
		if err != nil {
			return api.MaintenanceCleanupCounts{}, fmt.Errorf("archived purge: %w", err)
		}
		demo, err := seeding.RemoveDemo(ctx, seeding.RemoveDemoDeps{
			DB:           gormDB,
			Artifacts:    artifactSvc,
			Integrations: integrationsRepo,
		})
		if err != nil {
			return api.MaintenanceCleanupCounts{}, fmt.Errorf("demo removal: %w", err)
		}
		sweep, err := retentionSweeper.Once(ctx)
		if err != nil {
			return api.MaintenanceCleanupCounts{}, fmt.Errorf("retention sweep: %w", err)
		}
		return api.MaintenanceCleanupCounts{
			ExpiredArtifactsDeleted:       sweep.Deleted,
			ReferencedSkipped:             sweep.SkippedReferenced,
			DemoFeaturesDeleted:           demo.FeaturesDeleted,
			DemoChangeRequestsDeleted:     demo.ChangeRequestsDeleted,
			DemoArtifactsDeleted:          demo.ArtifactsDeleted,
			ArchivedChangeRequestsDeleted: archived,
		}, nil
	}
	maintenanceDemoRemove := func(ctx context.Context, workspaceID string) (api.MaintenanceDemoRemoveCounts, error) {
		ctx = artifact.WithWorkspace(ctx, workspaceID)
		demo, err := seeding.RemoveDemo(ctx, seeding.RemoveDemoDeps{
			DB:           gormDB,
			Artifacts:    artifactSvc,
			Integrations: integrationsRepo,
		})
		if err != nil {
			return api.MaintenanceDemoRemoveCounts{}, fmt.Errorf("demo removal: %w", err)
		}
		return api.MaintenanceDemoRemoveCounts{
			FeaturesDeleted:       demo.FeaturesDeleted,
			ChangeRequestsDeleted: demo.ChangeRequestsDeleted,
			ArtifactsDeleted:      demo.ArtifactsDeleted,
			IntegrationsDeleted:   demo.IntegrationsDeleted,
			KnowledgeDeleted:      demo.KnowledgeDeleted,
			FeedbackDeleted:       demo.FeedbackDeleted,
		}, nil
	}

	handlers := &api.Handlers{
		Artifacts:                artifactSvc,
		WorkBoard:                workBoardRepo,
		Knowledge:                knowledgeSvc,
		S3:                       s3Client,
		BlobStore:                blobStore,
		GovernanceUploadPutTTL:   cfg.GovernanceUploadPutTTL,
		GovernanceFiles:          governanceFilesRepo,
		ArtifactAttachments:      artifactAttachmentsRepo,
		GovernanceUploadMaxBytes: cfg.GovernanceUploadMaxBytes,
		S3KeyPrefix:              cfg.S3.KeyPrefix,
		Settings:                 settingsSvc,
		Skills:                   skillsSvc,
		Identity:                 identityRepo,
		SeedWorkspaceSkills: func(ctx context.Context, workspaceID string) error {
			_, err := seeding.SeedSkills(
				skills.WithWorkspace(ctx, workspaceID),
				skillsSvc,
				settingsSvc,
				&log,
			)
			return err
		},
		Integrations: integrationsSvc,
		AppBaseURL:   cfg.PublicAppBaseURL,
		SchemaStatus: func(ctx context.Context) (api.SchemaStatusDTO, error) {
			status, err := storagedb.RequiredSchemaStatus(ctx, gormDB)
			if err != nil {
				return api.SchemaStatusDTO{}, err
			}
			return api.SchemaStatusDTO{
				Status:  status.Status,
				Message: status.Message,
				Missing: status.Missing,
			}, nil
		},
		OAuthCallbackBaseURL:    cfg.OAuth.PublicCallbackBaseURL,
		Governance:              govSvc,
		GovernanceChatHealth:    governanceChatHealth,
		GateTaskStore:           gateTaskStore,
		Config:                  cfg,
		MaintenanceCleanupFn:    maintenanceCleanup,
		MaintenanceDemoRemoveFn: maintenanceDemoRemove,
	}
	rt := &api.Router{
		Handlers: handlers,
		Config:   cfg,
		Logger:   &log,
	}
	if cfg.Sentry.DSN != "" {
		rt.SentryMiddleware = observability.SentryHTTPMiddleware()
	}
	router := rt.Build()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.DevCORS(router),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info().
			Str("addr", cfg.HTTPAddr).
			Str("docs", "/docs").
			Str("openapi", "/openapi.json").
			Msg("http server listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("http server")
		}
	}()

	if s3Client != nil {
		// Falls back to 24h when the env var is unset / non-positive (per spec §5.4).
		interval := cfg.GovernanceFilesPurgeIntervalDur
		if interval <= 0 {
			interval = 24 * time.Hour
		}
		purger := &governancefiles.Purger{
			Store:      governanceFilesRepo,
			S3:         governanceFileObjectDeleter,
			ReadyTTL:   time.Duration(settingsSvc.GetInt(settings.KeyGovernanceFilesTTLDays, 90)) * 24 * time.Hour,
			PendingTTL: time.Hour,
			Interval:   interval,
			// Read the TTL from settings on every sweep so a change in the
			// Settings UI takes effect without a restart.
			TTLDays: func() int { return settingsSvc.GetInt(settings.KeyGovernanceFilesTTLDays, 90) },
		}
		go purger.Run(ctx)
		log.Info().
			Int("ttl_days", settingsSvc.GetInt(settings.KeyGovernanceFilesTTLDays, 90)).
			Dur("interval", interval).
			Msg("governance_files: purger started")
	}

	// The sweeper loop always runs; each tick re-reads the enable toggle
	// (env flag or retention.artifact_sweep_enabled setting).
	go retentionSweeper.Run(ctx)
	log.Info().
		Bool("env_enabled", cfg.ArtifactRetentionSweepEnabled).
		Dur("interval", cfg.ArtifactRetentionSweepIntervalDur).
		Msg("retention: artifact sweeper loop started (ticks honor the enable toggle)")

	<-ctx.Done()
	log.Info().Msg("shutdown initiated")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("http shutdown")
	}
	if webhookWorker != nil {
		// Drains in-flight webhook + knowledge-ingest tasks before stopping.
		webhookWorker.Shutdown()
		if webhookClient != nil {
			_ = webhookClient.Close()
		}
		if knowledgeClient != nil {
			_ = knowledgeClient.Close()
		}
	}
}

// embeddingAPIKey returns the stored API key for the embedding provider. The
// embedding model reuses the same per-provider *.api_key settings as the chat
// models (no dedicated embedding key).
func embeddingAPIKey(s *settings.Service, provider string) string {
	switch provider {
	case "openai":
		return s.Get(settings.KeyOpenAIAPIKey)
	case "google_genai":
		return s.Get(settings.KeyGoogleAPIKey)
	case "openrouter":
		return s.Get(settings.KeyOpenRouterAPIKey)
	default:
		return ""
	}
}
