package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
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
	mcpmod "github.com/specgate/doc-registry/internal/mcp"
	"github.com/specgate/doc-registry/internal/observability"
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

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply migrations then exit")
	seedSkillsFlag := flag.Bool("seed-skills", false, "register missing gate-rubric skills, then exit")
	seedSkillsOverwriteFlag := flag.Bool("seed-skills-overwrite", false, "with --seed-skills, update existing starter skills by stable name")
	seedDemoFlag := flag.Bool("seed-demo", false, "create local demo planning data for UI development, then exit")
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

	gormDB, err := storagedb.Open(cfg.Database)
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
	if *migrateOnly {
		log.Info().Msg("migrations applied; exiting (--migrate-only)")
		return
	}

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

	// Auto-generate the MCP access token when none is configured (no MCP_API_KEY
	// env and no mcp.api_key setting), so the streamable MCP endpoint is gated
	// without manual setup. Idempotent; the value is retrievable via
	// GET /mcp/api-key (and the Settings → MCP UI), so it is not logged.
	if generated, err := mcpmod.EnsureAPIKey(settingsSvc); err != nil {
		log.Warn().Err(err).Msg("could not auto-generate mcp.api_key")
	} else if generated {
		log.Info().Msg("generated mcp.api_key — view/copy it in Settings → MCP or GET /mcp/api-key")
	}

	skillsSvc := skills.NewService(storagedb.NewSkillRepository(gormDB))
	integrationsRepo := storagedb.NewIntegrationRepository(gormDB)

	if *seedSkillsFlag {
		result, err := seeding.SeedSkillsWithOptions(context.Background(), skillsSvc, settingsSvc, &log, seeding.SkillSeedOptions{
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
	if result, err := seeding.SeedSkills(context.Background(), skillsSvc, settingsSvc, &log); err != nil {
		log.Warn().Err(err).Msg("auto-seed gate-rubric skills failed — quality gates may be missing rubrics")
	} else if len(result.SkillsCreated) > 0 || len(result.SkillsUpdated) > 0 {
		log.Info().
			Int("created", len(result.SkillsCreated)).
			Int("updated", len(result.SkillsUpdated)).
			Strs("new", result.SkillsCreated).
			Msg("gate-rubric skills synced on startup")
	}

	mcpBootAddr := settingsSvc.Get(settings.KeyMCPAddr)
	// MCP_ENABLED env var overrides the settings value (12-factor). Settings
	// remain the fallback so existing DBs keep working without env changes.
	mcpBootEnabled := mcpmod.ResolveEnabled(settingsSvc)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var blobStore blob.Store
	var s3Client *s3.Client
	var artifactObjectStore artifact.ObjectStore

	switch cfg.Blob.Driver {
	case "s3":
		s3Client, err = s3.New(ctx, cfg.S3)
		if err != nil {
			log.Fatal().Err(err).Msg("init s3 client")
		}
		artifactObjectStore = s3Client
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
	}
	// blobStore is wired into governance file upload/serve and knowledge upload paths.

	repo := storagedb.NewRepository(gormDB)
	artifactEditRepo := storagedb.NewArtifactEditRepository(gormDB)
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
	switch cfg.Queue.Driver {
	case "redis":
		if cfg.Redis.URL == "" {
			log.Fatal().Msg("QUEUE_DRIVER=redis requires REDIS_URL to be set")
		}
		redisOpt, err := asynq.ParseRedisURI(cfg.Redis.URL)
		if err != nil {
			log.Fatal().Err(err).Msg("parse REDIS_URL for webhook queue")
		}
		webhookClient = webhookqueue.NewClient(redisOpt, cfg.WebhookQueue.MaxRetry)
		webhookEnqueuer = webhookClient
		webhookWorker = asynq.NewServer(redisOpt, asynq.Config{
			Concurrency: cfg.WebhookQueue.Concurrency,
			Queues:      map[string]int{webhookqueue.QueueName: 1},
		})
	default: // "sync"
		log.Info().Msg("queue driver: sync (inline webhook processing, no Redis required)")
	}

	integrationsSvc := integrations.NewServiceWithWorkBoard(integrationsRepo, workBoardRepo).WithOAuthAppLookup(func(_ context.Context, provider string, hostKey string) (*integrations.OAuthAppConfig, error) {
		var creds config.OAuthAppCredentials
		switch provider {
		case integrations.ProviderGitHub:
			creds = cfg.OAuth.GitHub
		case integrations.ProviderGitLab:
			creds = cfg.OAuth.GitLab
		case integrations.ProviderLinear:
			creds = cfg.OAuth.Linear
		}
		if creds.ClientID == "" || creds.ClientSecret == "" {
			// Not configured — the OAuth flow treats a nil app as a validation error.
			return nil, nil
		}
		return &integrations.OAuthAppConfig{
			Provider:     provider,
			HostKey:      hostKey,
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
		}, nil
	}).WithWebhookSecrets(integrations.WebhookSecrets{
		Linear: cfg.Webhooks.Linear,
	}).WithWebhookEnqueuer(webhookEnqueuer)

	// Start the async webhook worker when QUEUE_DRIVER=redis. Dispatches enqueued
	// deliveries back through integrationsSvc.ProcessWebhookDelivery.
	if webhookWorker != nil {
		mux := asynq.NewServeMux()
		mux.HandleFunc(webhookqueue.TaskTypeWebhookDeliver, webhookqueue.Handler(integrationsSvc))
		if err := webhookWorker.Start(mux); err != nil {
			log.Fatal().Err(err).Msg("start webhook queue worker")
		}
		log.Info().Int("concurrency", cfg.WebhookQueue.Concurrency).Msg("webhook queue: async worker started")
	}
	governanceFilesRepo := storagedb.NewGovernanceFilesRepository(gormDB)
	governanceThreadsRepo := storagedb.NewGovernanceThreadsRepository(gormDB)
	artifactAttachmentsRepo := storagedb.NewArtifactAttachmentRepository(gormDB)
	knowledgeRepo := storagedb.NewKnowledgeRepository(gormDB)
	identityRepo := storagedb.NewIdentityRepository(gormDB)
	artifactObjectKey := func(featureID, version, filename string) string {
		return s3.ObjectKey(cfg.S3.KeyPrefix, featureID, version, filename)
	}
	artifactSvc := artifact.NewService(repo, artifactObjectStore, artifactObjectKey, cfg.S3.SignedURLTTL)
	governanceProfilesRepo := storagedb.NewGovernanceProfileRepository(gormDB)
	governanceProfilesSvc := governanceprofile.NewService(governanceProfilesRepo)
	if *seedDemoFlag {
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
		pvStore, err := pgvector.New(ctx, cfg.Database.PostgresDSN, cfg.Knowledge.EmbeddingDim)
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
	// Embeddings are configured in Settings → Model (provider + model + that
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
		log.Warn().Msg("knowledge embeddings disabled — set an Embedding Model + provider key in Settings → Model to enable knowledge search/upload")
	}
	// Raw document bytes go to S3 when configured; local dev uses NullObjectStore
	// (vector chunks in pgvector are the source of truth for knowledge search).
	var knowledgeObjectStore knowledge.ObjectStore
	if s3Client != nil {
		knowledgeObjectStore = s3Client
	} else {
		knowledgeObjectStore = knowledge.NullObjectStore{}
	}
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

	// Soft edge to the agents service: when AGENTS_BASE_URL is unset, all runners
	// stay true nil interfaces so the corresponding MCP tools are not registered.
	var (
		readinessRunner      mcpmod.ReadinessRunner
		llmGatesRunner       mcpmod.LLMGatesRunner
		deliveryReviewTrig   mcpmod.DeliveryReviewTrigger
		quickWorkItemCreator mcpmod.QuickWorkItemCreator
	)
	agentsClient := agentsclient.New(cfg.AgentsBaseURL)
	if agentsClient != nil {
		readinessRunner = agentsClient
		llmGatesRunner = agentsClient
		deliveryReviewTrig = agentsClient
		quickWorkItemCreator = agentsClient
	}

	govSvc := &governanceops.Service{
		WorkBoard:       workBoardRepo,
		Trackers:        integrationsSvc,
		AppBaseURL:      cfg.PublicAppBaseURL,
		Artifacts:       artifactSvc,
		Attachments:     artifactAttachmentsRepo,
		Skills:          skillsSvc,
		Knowledge:       knowledgeRepo,
		FeedbackStore:   integrationsSvc,
		ArtifactWriter:  artifactSvc,
		FeatureUpserter: workBoardRepo,
		ProfileResolver: governanceProfilesSvc,
		DraftArtifacts:  artifactSvc,
		EditStore:       artifactEditRepo,
		AgentsRunner:    agentsClient,
		StatsSource:     workBoardRepo,
	}

	// Shared between HTTP API and MCP server so both surfaces read/write the same tasks.
	// storagedb.Open is fatal above, so gormDB is always non-nil here.
	gateTaskStore := storagedb.NewPGGateTaskStore(gormDB)

	handlers := &api.Handlers{
		Artifacts:                artifactSvc,
		ArtifactEdit:             artifactEditRepo,
		WorkBoard:                workBoardRepo,
		Knowledge:                knowledgeSvc,
		S3:                       s3Client,
		BlobStore:                blobStore,
		GovernanceUploadPutTTL:   cfg.GovernanceUploadPutTTL,
		GovernanceFiles:          governanceFilesRepo,
		GovernanceThreads:        governanceThreadsRepo,
		ArtifactAttachments:      artifactAttachmentsRepo,
		GovernanceUploadMaxBytes: cfg.GovernanceUploadMaxBytes,
		S3KeyPrefix:              cfg.S3.KeyPrefix,
		Settings:                 settingsSvc,
		MCPBootEnabled:           mcpBootEnabled,
		Skills:                   skillsSvc,
		Identity:                 identityRepo,
		Integrations:             integrationsSvc,
		GovernanceProfiles:       governanceProfilesSvc,
		AppBaseURL:               cfg.PublicAppBaseURL,
		OAuthCallbackBaseURL:     cfg.OAuth.PublicCallbackBaseURL,
		Readiness:                readinessRunner,
		LLMGates:                 llmGatesRunner,
		DeliveryReview:           deliveryReviewTrig,
		QuickWorkItem:            quickWorkItemCreator,
		Governance:               govSvc,
		GateTaskStore:            gateTaskStore,
		Config:                   cfg,
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
			S3:         s3Client,
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

	var mcpSrv *http.Server
	if mcpBootEnabled && mcpBootAddr != "" {
		mcpHandler := mcpmod.NewDynamicMCPHandler(mcpmod.MCPHandlerOptions{
			Settings:            settingsSvc,
			IntegrationRepos:    api.NewIntegrationRepoSource(integrationsSvc),
			Knowledge:           knowledgeSvc,
			Artifacts:           artifactSvc,
			ArtifactEdit:        artifactEditRepo,
			WorkBoard:           workBoardRepo,
			Skills:              skillsSvc,
			Feedback:            integrationsSvc,
			TrackerLinks:        integrationsSvc,
			Profiles:            governanceProfilesSvc,
			Attachments:         artifactAttachmentsRepo,
			KnowledgeProvenance: knowledgeRepo,
			AppBaseURL:          cfg.PublicAppBaseURL,
			Readiness:           readinessRunner,
			LLMGates:            llmGatesRunner,
			DeliveryReview:      deliveryReviewTrig,
			QuickWorkItem:       quickWorkItemCreator,
		})
		mcpSrv = &http.Server{
			Addr:              mcpBootAddr,
			Handler:           mcpHandler,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Info().Str("addr", mcpBootAddr).Msg("mcp server listening (settings-driven)")
			if err := mcpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal().Err(err).Msg("mcp server")
			}
		}()
	} else {
		log.Info().Msg("MCP not listening (mcp.enabled=false or mcp.addr empty). Change via PUT /settings and restart to activate.")
	}

	<-ctx.Done()
	log.Info().Msg("shutdown initiated")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if mcpSrv != nil {
		if err := mcpSrv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("mcp shutdown")
		}
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("http shutdown")
	}
	if webhookWorker != nil {
		// Drains in-flight webhook tasks before stopping.
		webhookWorker.Shutdown()
		if webhookClient != nil {
			_ = webhookClient.Close()
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
