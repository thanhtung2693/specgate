package config

import (
	"os"
	"testing"
	"time"
)

// requirePostgresDSN ensures POSTGRES_DSN is set to a placeholder so Load()
// validation does not fail for tests that focus on non-database config fields.
// Tests that explicitly test database validation (TestLoad_PostgresRequiresDSN,
// TestLoad_EmptyDriverRejected, etc.) set their own values and do not call this.
func requirePostgresDSN(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")
}

func TestLoad_Defaults(t *testing.T) {
	// Clear env keys that might affect the test if inherited from the shell.
	keys := []string{
		"HTTP_ADDR", "S3_SIGNED_URL_TTL",
		"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY",
		"S3_USE_PATH_STYLE", "S3_ENSURE_BUCKET",
		"OPENAPI_ENABLED",
		"SENTRY_DSN", "SENTRY_ENVIRONMENT", "SENTRY_RELEASE", "SENTRY_TRACES_SAMPLE_RATE",
		"APP_ENV", "LOG_LEVEL", "LOG_FORMAT",
		"S3_GOVERNANCE_UPLOAD_PUT_TTL",
		"GOVERNANCE_UPLOAD_MAX_BYTES", "GOVERNANCE_FILES_PURGE_INTERVAL_HOURS",
		"KNOWLEDGE_MAX_FILE_BYTES", "KNOWLEDGE_EMBEDDING_MODEL", "KNOWLEDGE_EMBEDDING_DIM",
		"SETTINGS_ENCRYPTION_KEY",
		"DATABASE_DRIVER", "POSTGRES_DSN",
	}
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
	// Default driver is now "postgres" which requires a non-empty DSN.
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("Database.Driver = %q, want postgres", cfg.Database.Driver)
	}
	if cfg.S3.Region != "us-east-1" {
		t.Errorf("S3.Region = %q", cfg.S3.Region)
	}
	if !cfg.S3.EnsureBucket {
		t.Errorf("S3.EnsureBucket = false, want true")
	}
	if cfg.S3.SignedURLTTL != 15*time.Minute {
		t.Errorf("SignedURLTTL = %v", cfg.S3.SignedURLTTL)
	}
	if cfg.GovernanceUploadPutTTL != 15*time.Minute {
		t.Errorf("GovernanceUploadPutTTL = %v", cfg.GovernanceUploadPutTTL)
	}
	if cfg.GovernanceUploadMaxBytes != 26214400 {
		t.Errorf("GovernanceUploadMaxBytes = %d, want 26214400", cfg.GovernanceUploadMaxBytes)
	}
	if cfg.GovernanceFilesPurgeIntervalDur != 24*time.Hour {
		t.Errorf("GovernanceFilesPurgeIntervalDur = %v, want 24h", cfg.GovernanceFilesPurgeIntervalDur)
	}
	if cfg.Sentry.DSN != "" {
		t.Errorf("Sentry.DSN = %q", cfg.Sentry.DSN)
	}
	if cfg.Sentry.TracesSampleRate != 0 {
		t.Errorf("Sentry.TracesSampleRate = %v", cfg.Sentry.TracesSampleRate)
	}
	if cfg.Knowledge.MaxFileBytes != 10485760 {
		t.Errorf("Knowledge.MaxFileBytes = %d", cfg.Knowledge.MaxFileBytes)
	}
	if cfg.Knowledge.EmbeddingDim != 1024 {
		t.Errorf("Knowledge.EmbeddingDim = %d", cfg.Knowledge.EmbeddingDim)
	}
}

func TestLoad_S3EnsureBucketFalse(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("S3_ENSURE_BUCKET", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.EnsureBucket {
		t.Fatal("S3.EnsureBucket should be false")
	}
}

func TestLoad_CustomEnv(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("HTTP_ADDR", ":3000")
	t.Setenv("S3_SIGNED_URL_TTL", "5m")
	t.Setenv("S3_REGION", "ap-southeast-1")
	t.Setenv("OPENAPI_ENABLED", "false")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("KNOWLEDGE_MAX_FILE_BYTES", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":3000" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.S3.SignedURLTTL != 5*time.Minute {
		t.Fatalf("SignedURLTTL = %v", cfg.S3.SignedURLTTL)
	}
	if cfg.S3.Region != "ap-southeast-1" {
		t.Fatalf("S3.Region = %q", cfg.S3.Region)
	}
	if cfg.OpenAPI.Enabled {
		t.Fatal("OpenAPI.Enabled should be false")
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.Knowledge.MaxFileBytes != 2048 {
		t.Fatalf("Knowledge.MaxFileBytes = %d", cfg.Knowledge.MaxFileBytes)
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
	t.Setenv("S3_SIGNED_URL_TTL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_InvalidGovernanceUploadTTL(t *testing.T) {
	t.Setenv("S3_GOVERNANCE_UPLOAD_PUT_TTL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_AppEnvDefault(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("APP_ENV")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "development" {
		t.Errorf("AppEnv = %q, want development", cfg.AppEnv)
	}
}

func TestLoad_AppEnvCustom(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("APP_ENV", "production")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "production" {
		t.Errorf("AppEnv = %q, want production", cfg.AppEnv)
	}
}

func TestLoad_PublicAppBaseURL(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("APP_BASE_URL")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicAppBaseURL != "http://localhost:3000" {
		t.Errorf("PublicAppBaseURL default = %q", cfg.PublicAppBaseURL)
	}

	t.Setenv("APP_BASE_URL", "https://app.specgate.example")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicAppBaseURL != "https://app.specgate.example" {
		t.Errorf("PublicAppBaseURL = %q", cfg.PublicAppBaseURL)
	}
}

func TestLoad_AgentsBaseURL(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("AGENTS_BASE_URL")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentsBaseURL != "" {
		t.Errorf("AgentsBaseURL default = %q, want empty", cfg.AgentsBaseURL)
	}

	t.Setenv("AGENTS_BASE_URL", "  http://agents:8000  ")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentsBaseURL != "http://agents:8000" {
		t.Errorf("AgentsBaseURL = %q, want trimmed http://agents:8000", cfg.AgentsBaseURL)
	}
}

func TestLoad_OAuthDefaults(t *testing.T) {
	requirePostgresDSN(t)
	for _, k := range []string{
		"OAUTH_PUBLIC_CALLBACK_BASE_URL",
		"GITHUB_OAUTH_CLIENT_ID", "GITHUB_OAUTH_CLIENT_SECRET",
		"GITLAB_OAUTH_CLIENT_ID", "GITLAB_OAUTH_CLIENT_SECRET",
		"LINEAR_OAUTH_CLIENT_ID", "LINEAR_OAUTH_CLIENT_SECRET",
	} {
		_ = os.Unsetenv(k)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.PublicCallbackBaseURL != "" {
		t.Errorf("OAuth.PublicCallbackBaseURL default = %q, want empty", cfg.OAuth.PublicCallbackBaseURL)
	}
	if cfg.OAuth.GitHub != (OAuthAppCredentials{}) {
		t.Errorf("OAuth.GitHub default = %#v, want zero", cfg.OAuth.GitHub)
	}
	if cfg.OAuth.GitLab != (OAuthAppCredentials{}) {
		t.Errorf("OAuth.GitLab default = %#v, want zero", cfg.OAuth.GitLab)
	}
	if cfg.OAuth.Linear != (OAuthAppCredentials{}) {
		t.Errorf("OAuth.Linear default = %#v, want zero", cfg.OAuth.Linear)
	}
}

func TestLoad_OAuthFromEnv(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("OAUTH_PUBLIC_CALLBACK_BASE_URL", "https://app.specgate.example")
	t.Setenv("GITHUB_OAUTH_CLIENT_ID", "gh-id")
	t.Setenv("GITHUB_OAUTH_CLIENT_SECRET", "gh-secret")
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "gl-id")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "gl-secret")
	t.Setenv("LINEAR_OAUTH_CLIENT_ID", "lin-id")
	t.Setenv("LINEAR_OAUTH_CLIENT_SECRET", "lin-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuth.PublicCallbackBaseURL != "https://app.specgate.example" {
		t.Errorf("OAuth.PublicCallbackBaseURL = %q", cfg.OAuth.PublicCallbackBaseURL)
	}
	if cfg.OAuth.GitHub != (OAuthAppCredentials{ClientID: "gh-id", ClientSecret: "gh-secret"}) {
		t.Errorf("OAuth.GitHub = %#v", cfg.OAuth.GitHub)
	}
	if cfg.OAuth.GitLab != (OAuthAppCredentials{ClientID: "gl-id", ClientSecret: "gl-secret"}) {
		t.Errorf("OAuth.GitLab = %#v", cfg.OAuth.GitLab)
	}
	if cfg.OAuth.Linear != (OAuthAppCredentials{ClientID: "lin-id", ClientSecret: "lin-secret"}) {
		t.Errorf("OAuth.Linear = %#v", cfg.OAuth.Linear)
	}
}

func TestLoad_WebhookSecretsDefaults(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("LINEAR_WEBHOOK_SECRET")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webhooks != (WebhookConfig{}) {
		t.Errorf("Webhooks default = %#v, want zero", cfg.Webhooks)
	}
}

// Only Linear is env-sourced now; GitLab/GitHub use a self-served
// per-integration secret, so there is no GITHUB_/GITLAB_WEBHOOK_SECRET to read.
func TestLoad_WebhookSecretsFromEnv(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("LINEAR_WEBHOOK_SECRET", "lin-wh")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webhooks != (WebhookConfig{Linear: "lin-wh"}) {
		t.Errorf("Webhooks = %#v", cfg.Webhooks)
	}
}

// APP_ENV flows into Sentry.Environment when SENTRY_ENVIRONMENT is unset,
// so one variable drives both tags.
func TestLoad_AppEnvFlowsIntoSentryEnvironment(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("APP_ENV", "staging")
	_ = os.Unsetenv("SENTRY_ENVIRONMENT")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sentry.Environment != "staging" {
		t.Errorf("Sentry.Environment = %q, want staging (fallback to APP_ENV)", cfg.Sentry.Environment)
	}
}

// Explicit SENTRY_ENVIRONMENT wins over APP_ENV.
func TestLoad_SentryEnvironmentOverridesAppEnv(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("SENTRY_ENVIRONMENT", "prod-eu")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sentry.Environment != "prod-eu" {
		t.Errorf("Sentry.Environment = %q, want prod-eu", cfg.Sentry.Environment)
	}
}

// Empty DATABASE_DRIVER is rejected (falls through the default arm of the switch).
func TestLoad_EmptyDriverRejected(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty DATABASE_DRIVER")
	}
}

func TestLoad_SettingsEncryptionKey(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("SETTINGS_ENCRYPTION_KEY", "aabbccdd")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SettingsEncryptionKey != "aabbccdd" {
		t.Fatalf("got %q", cfg.SettingsEncryptionKey)
	}
}

func TestGetEnvBool(t *testing.T) {
	t.Setenv("TEST_BOOL_X", "true")
	if !getEnvBool("TEST_BOOL_X", false) {
		t.Fatal("expected true")
	}
	t.Setenv("TEST_BOOL_X", "false")
	if getEnvBool("TEST_BOOL_X", true) {
		t.Fatal("expected false")
	}
	_ = os.Unsetenv("TEST_BOOL_X")
	if !getEnvBool("TEST_BOOL_X", true) {
		t.Fatal("expected default true")
	}
}

func TestGetEnvFloat(t *testing.T) {
	t.Setenv("TEST_FLOAT_X", "0.25")
	if getEnvFloat("TEST_FLOAT_X", 0) != 0.25 {
		t.Fatal("expected 0.25")
	}
	t.Setenv("TEST_FLOAT_X", "not-float")
	if getEnvFloat("TEST_FLOAT_X", 0.5) != 0.5 {
		t.Fatal("expected default on parse error")
	}
}

func TestGetEnvInt(t *testing.T) {
	t.Setenv("TEST_INT_X", "42")
	if getEnvInt("TEST_INT_X", 0) != 42 {
		t.Fatal("expected 42")
	}
	t.Setenv("TEST_INT_X", "not-int")
	if getEnvInt("TEST_INT_X", 7) != 7 {
		t.Fatal("expected default on parse error")
	}
}

func TestLoad_PostgresRequiresDSN(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "postgres")
	_ = os.Unsetenv("POSTGRES_DSN")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when DATABASE_DRIVER=postgres and POSTGRES_DSN empty")
	}
}

func TestLoad_PostgresDSN(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("POSTGRES_DSN", "postgres://u:p@localhost:5432/db?sslmode=disable")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("Driver = %q", cfg.Database.Driver)
	}
	if cfg.Database.PostgresDSN != "postgres://u:p@localhost:5432/db?sslmode=disable" {
		t.Errorf("PostgresDSN = %q", cfg.Database.PostgresDSN)
	}
}

func TestLoad_UnknownDriverRejected(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "mongo")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}

func TestQueueDriverDefault(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("QUEUE_DRIVER")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Queue.Driver != "sync" {
		t.Errorf("expected default Queue.Driver=sync, got %q", cfg.Queue.Driver)
	}
}

func TestQueueDriverRedis(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("QUEUE_DRIVER", "redis")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Queue.Driver != "redis" {
		t.Errorf("expected Queue.Driver=redis, got %q", cfg.Queue.Driver)
	}
}

func TestKnowledgeDriverDefault(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("KNOWLEDGE_DRIVER")
	// Knowledge/vector search is an opt-in experimental feature: the driver
	// defaults to "none" (disabled). Set KNOWLEDGE_DRIVER=pgvector to enable it.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Knowledge.Driver != "none" {
		t.Errorf("expected default Knowledge.Driver=none, got %q", cfg.Knowledge.Driver)
	}
}

func TestKnowledgeDriverNone(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("KNOWLEDGE_DRIVER", "none")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Knowledge.Driver != "none" {
		t.Errorf("expected Knowledge.Driver=none, got %q", cfg.Knowledge.Driver)
	}
}

func TestStorageDriverDefault(t *testing.T) {
	requirePostgresDSN(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Blob.Driver != "local" {
		t.Errorf("expected local, got %q", cfg.Blob.Driver)
	}
	if cfg.Blob.DataRoot == "" {
		t.Error("Blob.DataRoot should not be empty")
	}
}

func TestLoad_KnowledgeDisabledByDefault(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")
	_ = os.Unsetenv("KNOWLEDGE_DRIVER")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Knowledge.Driver != "none" {
		t.Errorf("Knowledge.Driver = %q, want none (experimental, opt-in)", cfg.Knowledge.Driver)
	}
}

func TestLoad_KnowledgeDriverOverrideWins(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")
	// An explicit driver enables the experimental vector search.
	t.Setenv("KNOWLEDGE_DRIVER", "pgvector")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Knowledge.Driver != "pgvector" {
		t.Errorf("explicit KNOWLEDGE_DRIVER ignored: got %q", cfg.Knowledge.Driver)
	}
}
