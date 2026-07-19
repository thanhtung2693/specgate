package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// BlobConfig selects the blob storage backend for artifact documents.
type BlobConfig struct {
	// "local" = local filesystem BlobStore (default, no MinIO required).
	// "s3"    = S3 / MinIO (requires S3_ENDPOINT + credentials).
	Driver   string
	DataRoot string
}

type Config struct {
	HTTPAddr string

	PostgresDSN string

	Blob BlobConfig

	// Presigned PUT TTL for governance-chat image uploads (S3 keys under governance/resources/uploads/).
	GovernanceUploadPutTTL time.Duration

	// GovernanceUploadMaxBytes is the maximum size accepted at upload and read time.
	GovernanceUploadMaxBytes int64
	// GovernanceFilesPurgeIntervalDur is how often the TTL purger sweeps (derived from GOVERNANCE_FILES_PURGE_INTERVAL_HOURS).
	GovernanceFilesPurgeIntervalDur time.Duration

	// ArtifactRetentionSweepEnabled turns on the opt-in artifact retention
	// sweeper (spec §9). Off by default: local-first deployments keep every
	// artifact unless the operator opts in.
	ArtifactRetentionSweepEnabled bool
	// ArtifactRetentionSweepIntervalDur is how often the retention sweeper runs
	// (derived from ARTIFACT_RETENTION_SWEEP_INTERVAL_HOURS).
	ArtifactRetentionSweepIntervalDur time.Duration

	S3        S3Config
	Knowledge KnowledgeConfig
	Redis     RedisConfig

	OpenAPI OpenAPIConfig

	Sentry SentryConfig

	// SettingsEncryptionKey is a 32-byte hex-encoded AES key for encrypting sensitive settings at rest.
	// Read from SETTINGS_ENCRYPTION_KEY; validated at startup in main (mandatory).
	SettingsEncryptionKey string

	// PublicAppBaseURL is the SpecGate UI origin used to build generated links.
	// Read from APP_BASE_URL. When absent, release-local stacks can provide
	// UI_PORT so upgraded CLIs still get side-by-side local review links.
	PublicAppBaseURL string

	// AgentsBaseURL is the internal base URL of the agents service (FastAPI).
	// When set, agents-backed API operations can run readiness checks,
	// quality gates, delivery review, and quick work creation. Empty (default)
	// disables those operations. Read from AGENTS_BASE_URL.
	AgentsBaseURL string

	// DeliverySLADays is the number of days after which a failing delivery review
	// triggers a delivery_stale stale warning. Read from DELIVERY_SLA_DAYS;
	// defaults to 7.
	DeliverySLADays int

	OAuth OAuthConfig

	// WebhookQueue tunes the async inbound-webhook worker (enabled when Redis.URL
	// is set).
	WebhookQueue WebhookQueueConfig

	// Queue selects the webhook queue back-end driver. "sync" (default) processes
	// webhooks inline without Redis. "redis" uses the asynq async queue and
	// requires Redis.URL to be set.
	Queue QueueConfig
}

// OAuthConfig holds the per-provider OAuth app credentials and the public
// callback origin, all sourced from environment variables. A provider whose
// ClientID/ClientSecret are empty is treated as "not configured" — the OAuth
// flow rejects connect attempts for it with a validation error.
type OAuthConfig struct {
	// PublicCallbackBaseURL is an OPTIONAL override for the public origin the
	// provider redirects back to (the callback path /integrations/oauth-callback
	// is appended). Empty by default: the origin is derived from the inbound
	// request, so local dev needs no config. Set OAUTH_PUBLIC_CALLBACK_BASE_URL
	// only behind a reverse proxy / in prod where the request host is not public.
	PublicCallbackBaseURL string
	GitHub                OAuthAppCredentials
	GitLab                OAuthAppCredentials
	Linear                OAuthAppCredentials
}

type OAuthAppCredentials struct {
	ClientID     string
	ClientSecret string
}

// SentryConfig is optional; when DSN is empty, Sentry is not initialized.
type SentryConfig struct {
	DSN              string
	Environment      string
	Release          string
	TracesSampleRate float64
}

type S3Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	// EnsureBucket runs HeadBucket/CreateBucket at client init (local MinIO has no pre-created bucket).
	// Set S3_ENSURE_BUCKET=false when the bucket is provisioned out-of-band.
	EnsureBucket bool
	// KeyPrefix is prepended to every object key (artifacts, governance files,
	// knowledge documents). Use this when the bucket is shared with other
	// services so doc-registry's content lives under a dedicated directory.
	// Defaults to "doc-registry/" — set S3_KEY_PREFIX="" to disable.
	KeyPrefix string
}

type RedisConfig struct {
	// URL is the Redis connection URI (redis://host:port/db). Empty disables.
	URL string
	// KeyPrefix is prepended to every key doc-registry writes to Redis so the
	// instance can be shared with other services without collision. Defaults to
	// "DOC_REGISTRY:" — set REDIS_KEY_PREFIX="" to disable.
	KeyPrefix string
}

// WebhookQueueConfig tunes the asynq-backed async inbound-webhook worker. The
// queue is enabled only when Redis.URL is set; otherwise webhooks process inline.
type WebhookQueueConfig struct {
	// Concurrency is the number of webhook tasks processed in parallel.
	Concurrency int
	// MaxRetry caps automatic retries per failed task before it is archived.
	MaxRetry int
}

// QueueConfig selects the webhook queue back-end.
type QueueConfig struct {
	// Driver is "sync" (default, no Redis required — webhooks process inline)
	// or "redis" (asynq async queue, requires Redis.URL to be set).
	Driver string
}

type KnowledgeConfig struct {
	MaxFileBytes int64
	// EmbeddingDim is the vector store collection width. The embedding provider
	// and model are configured in Settings → Models, not via env.
	EmbeddingDim int
	// Driver selects the vector store backend.
	// "pgvector" (default) — PostgreSQL + pgvector extension.
	// "none"               — vector search disabled; queries return empty results.
	Driver string
}

type OpenAPIConfig struct {
	Enabled bool
}

func Load() (*Config, error) {
	governancePutTTL, err := time.ParseDuration(getEnv("S3_GOVERNANCE_UPLOAD_PUT_TTL", "15m"))
	if err != nil {
		return nil, fmt.Errorf("S3_GOVERNANCE_UPLOAD_PUT_TTL: %w", err)
	}

	maxBytes := getEnvInt64("GOVERNANCE_UPLOAD_MAX_BYTES", 26214400) // 25 MiB default
	knowledgeMaxBytes := getEnvInt64("KNOWLEDGE_MAX_FILE_BYTES", 10485760)
	if maxBytes <= 0 {
		return nil, fmt.Errorf("GOVERNANCE_UPLOAD_MAX_BYTES must be positive")
	}
	if knowledgeMaxBytes <= 0 {
		return nil, fmt.Errorf("KNOWLEDGE_MAX_FILE_BYTES must be positive")
	}
	purgeHours := getEnvFloat("GOVERNANCE_FILES_PURGE_INTERVAL_HOURS", 24)
	retentionSweepHours := getEnvFloat("ARTIFACT_RETENTION_SWEEP_INTERVAL_HOURS", 24)

	cfg := &Config{
		HTTPAddr:                        getEnv("HTTP_ADDR", ":8080"),
		PostgresDSN:                     getEnv("POSTGRES_DSN", ""),
		GovernanceUploadPutTTL:          governancePutTTL,
		GovernanceUploadMaxBytes:        maxBytes,
		GovernanceFilesPurgeIntervalDur: time.Duration(purgeHours * float64(time.Hour)),

		ArtifactRetentionSweepEnabled:     getEnvBool("ARTIFACT_RETENTION_SWEEP_ENABLED", false),
		ArtifactRetentionSweepIntervalDur: time.Duration(retentionSweepHours * float64(time.Hour)),
		S3: S3Config{
			Endpoint:     getEnv("S3_ENDPOINT", ""),
			Region:       getEnv("S3_REGION", "us-east-1"),
			Bucket:       getEnv("S3_BUCKET", "doc-registry"),
			AccessKey:    getEnv("S3_ACCESS_KEY", ""),
			SecretKey:    getEnv("S3_SECRET_KEY", ""),
			UsePathStyle: getEnvBool("S3_USE_PATH_STYLE", true),
			EnsureBucket: getEnvBool("S3_ENSURE_BUCKET", true),
			KeyPrefix:    normalizeKeyPrefix(getEnv("S3_KEY_PREFIX", "doc-registry/")),
		},
		Redis: RedisConfig{
			URL:       getEnv("REDIS_URL", ""),
			KeyPrefix: normalizeRedisPrefix(getEnv("REDIS_KEY_PREFIX", "DOC_REGISTRY:")),
		},
		Knowledge: KnowledgeConfig{
			MaxFileBytes: knowledgeMaxBytes,
			EmbeddingDim: getEnvInt("KNOWLEDGE_EMBEDDING_DIM", 1024),
			Driver:       getEnv("KNOWLEDGE_DRIVER", "none"),
		},
		Blob: BlobConfig{
			Driver:   getEnv("STORAGE_DRIVER", "local"),
			DataRoot: getEnv("BLOB_DATA_ROOT", "/data/blobs"),
		},
		OpenAPI: OpenAPIConfig{
			Enabled: getEnvBool("OPENAPI_ENABLED", true),
		},
	}

	cfg.SettingsEncryptionKey = getEnv("SETTINGS_ENCRYPTION_KEY", "")
	cfg.PublicAppBaseURL = publicAppBaseURL()
	cfg.AgentsBaseURL = strings.TrimSpace(getEnv("AGENTS_BASE_URL", ""))
	cfg.DeliverySLADays = getEnvInt("DELIVERY_SLA_DAYS", 7)

	cfg.OAuth = OAuthConfig{
		PublicCallbackBaseURL: strings.TrimSpace(getEnv("OAUTH_PUBLIC_CALLBACK_BASE_URL", "")),
		GitHub: OAuthAppCredentials{
			ClientID:     strings.TrimSpace(getEnv("GITHUB_OAUTH_CLIENT_ID", "")),
			ClientSecret: strings.TrimSpace(getEnv("GITHUB_OAUTH_CLIENT_SECRET", "")),
		},
		GitLab: OAuthAppCredentials{
			ClientID:     strings.TrimSpace(getEnv("GITLAB_OAUTH_CLIENT_ID", "")),
			ClientSecret: strings.TrimSpace(getEnv("GITLAB_OAUTH_CLIENT_SECRET", "")),
		},
		Linear: OAuthAppCredentials{
			ClientID:     strings.TrimSpace(getEnv("LINEAR_OAUTH_CLIENT_ID", "")),
			ClientSecret: strings.TrimSpace(getEnv("LINEAR_OAUTH_CLIENT_SECRET", "")),
		},
	}

	cfg.WebhookQueue = WebhookQueueConfig{
		Concurrency: getEnvInt("WEBHOOK_QUEUE_CONCURRENCY", 10),
		MaxRetry:    getEnvInt("WEBHOOK_QUEUE_MAX_RETRY", 5),
	}

	cfg.Queue = QueueConfig{
		Driver: getEnv("QUEUE_DRIVER", "sync"),
	}

	cfg.Sentry = SentryConfig{
		DSN:              strings.TrimSpace(getEnv("SENTRY_DSN", "")),
		Environment:      getEnv("SENTRY_ENVIRONMENT", "development"),
		Release:          strings.TrimSpace(getEnv("SENTRY_RELEASE", "")),
		TracesSampleRate: getEnvFloat("SENTRY_TRACES_SAMPLE_RATE", 0),
	}

	if strings.TrimSpace(cfg.PostgresDSN) == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}

	return cfg, nil
}

func getEnv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func publicAppBaseURL() string {
	if raw := strings.TrimSpace(getEnv("APP_BASE_URL", "")); raw != "" {
		return raw
	}
	if port := strings.TrimSpace(getEnv("UI_PORT", "")); port != "" {
		return "http://localhost:" + port
	}
	return "http://localhost:3000"
}

func getEnvInt(k string, def int) int {
	if v, ok := os.LookupEnv(k); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvInt64(k string, def int64) int64 {
	if v, ok := os.LookupEnv(k); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(k string, def bool) bool {
	if v, ok := os.LookupEnv(k); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// normalizeKeyPrefix trims whitespace + leading slashes and ensures the
// result either is empty or ends with exactly one trailing slash so callers
// can prepend it directly to an object key without double-slash artifacts.
func normalizeKeyPrefix(raw string) string {
	prefix := strings.TrimSpace(raw)
	prefix = strings.TrimLeft(prefix, "/")
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

// normalizeRedisPrefix trims whitespace and ensures the result either is empty
// or ends with exactly one trailing colon (Redis-idiomatic separator) so
// callers can prepend it directly to a key without double-colon artifacts.
func normalizeRedisPrefix(raw string) string {
	prefix := strings.TrimSpace(raw)
	if prefix == "" {
		return ""
	}
	prefix = strings.TrimRight(prefix, ":")
	return prefix + ":"
}

func getEnvFloat(k string, def float64) float64 {
	if v, ok := os.LookupEnv(k); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
