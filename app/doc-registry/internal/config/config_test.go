package config

import (
	"os"
	"testing"
	"time"
)

// requirePostgresDSN ensures POSTGRES_DSN is set to a placeholder so Load()
// validation does not fail for tests that focus on non-database config fields.
// Tests that explicitly test database validation set their own values and do
// not call this.
func requirePostgresDSN(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")
}

func TestLoad_Defaults(t *testing.T) {
	// Clear env keys that might affect the test if inherited from the shell.
	keys := []string{
		"HTTP_ADDR",
		"S3_ENDPOINT", "S3_REGION", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY",
		"S3_USE_PATH_STYLE", "S3_ENSURE_BUCKET",
		"OPENAPI_ENABLED",
		"SENTRY_DSN", "SENTRY_ENVIRONMENT", "SENTRY_RELEASE", "SENTRY_TRACES_SAMPLE_RATE",
		"S3_GOVERNANCE_UPLOAD_PUT_TTL",
		"GOVERNANCE_UPLOAD_MAX_BYTES", "GOVERNANCE_FILES_PURGE_INTERVAL_HOURS",
		"ARTIFACT_RETENTION_SWEEP_ENABLED", "ARTIFACT_RETENTION_SWEEP_INTERVAL_HOURS",
		"KNOWLEDGE_MAX_FILE_BYTES", "KNOWLEDGE_EMBEDDING_DIM",
		"DELIVERY_SLA_DAYS",
		"SETTINGS_ENCRYPTION_KEY",
		"POSTGRES_DSN",
	}
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.S3.Region != "us-east-1" {
		t.Errorf("S3.Region = %q", cfg.S3.Region)
	}
	if !cfg.S3.EnsureBucket {
		t.Errorf("S3.EnsureBucket = false, want true")
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
	if cfg.DeliverySLADays != 7 {
		t.Errorf("DeliverySLADays = %d, want 7", cfg.DeliverySLADays)
	}
	if cfg.ArtifactRetentionSweepEnabled {
		t.Error("ArtifactRetentionSweepEnabled = true, want false (opt-in)")
	}
	if cfg.ArtifactRetentionSweepIntervalDur != 24*time.Hour {
		t.Errorf("ArtifactRetentionSweepIntervalDur = %v, want 24h", cfg.ArtifactRetentionSweepIntervalDur)
	}
}

func TestLoad_ArtifactRetentionSweepEnv(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("ARTIFACT_RETENTION_SWEEP_ENABLED", "true")
	t.Setenv("ARTIFACT_RETENTION_SWEEP_INTERVAL_HOURS", "6")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ArtifactRetentionSweepEnabled {
		t.Fatal("ArtifactRetentionSweepEnabled should be true")
	}
	if cfg.ArtifactRetentionSweepIntervalDur != 6*time.Hour {
		t.Fatalf("ArtifactRetentionSweepIntervalDur = %v, want 6h", cfg.ArtifactRetentionSweepIntervalDur)
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
	t.Setenv("S3_REGION", "ap-southeast-1")
	t.Setenv("OPENAPI_ENABLED", "false")
	t.Setenv("KNOWLEDGE_MAX_FILE_BYTES", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":3000" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.S3.Region != "ap-southeast-1" {
		t.Fatalf("S3.Region = %q", cfg.S3.Region)
	}
	if cfg.OpenAPI.Enabled {
		t.Fatal("OpenAPI.Enabled should be false")
	}
	if cfg.Knowledge.MaxFileBytes != 2048 {
		t.Fatalf("Knowledge.MaxFileBytes = %d", cfg.Knowledge.MaxFileBytes)
	}
}

func TestLoad_InvalidGovernanceUploadTTL(t *testing.T) {
	t.Setenv("S3_GOVERNANCE_UPLOAD_PUT_TTL", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_SentryEnvironmentDefaultsDevelopment(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("SENTRY_ENVIRONMENT")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sentry.Environment != "development" {
		t.Errorf("Sentry.Environment = %q, want development", cfg.Sentry.Environment)
	}
}

func TestLoad_PublicAppBaseURL(t *testing.T) {
	requirePostgresDSN(t)
	_ = os.Unsetenv("APP_BASE_URL")
	_ = os.Unsetenv("UI_PORT")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicAppBaseURL != "http://localhost:3000" {
		t.Errorf("PublicAppBaseURL default = %q", cfg.PublicAppBaseURL)
	}

	t.Setenv("UI_PORT", "13105")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicAppBaseURL != "http://localhost:13105" {
		t.Errorf("PublicAppBaseURL with UI_PORT = %q", cfg.PublicAppBaseURL)
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

func TestLoad_DeliverySLADays(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("DELIVERY_SLA_DAYS", "14")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeliverySLADays != 14 {
		t.Errorf("DeliverySLADays = %d, want 14", cfg.DeliverySLADays)
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

func TestLoad_SentryEnvironment(t *testing.T) {
	requirePostgresDSN(t)
	t.Setenv("SENTRY_ENVIRONMENT", "prod-eu")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sentry.Environment != "prod-eu" {
		t.Errorf("Sentry.Environment = %q, want prod-eu", cfg.Sentry.Environment)
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
	_ = os.Unsetenv("POSTGRES_DSN")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when POSTGRES_DSN is empty")
	}
}

func TestLoad_PostgresDSN(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://u:p@localhost:5432/db?sslmode=disable")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://u:p@localhost:5432/db?sslmode=disable" {
		t.Errorf("PostgresDSN = %q", cfg.PostgresDSN)
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
	// Knowledge vector search is alpha opt-in: the driver defaults to "none"
	// (disabled). Set KNOWLEDGE_DRIVER=pgvector to enable it.
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
		t.Errorf("Knowledge.Driver = %q, want none (alpha opt-in)", cfg.Knowledge.Driver)
	}
}

func TestLoad_KnowledgeDriverOverrideWins(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/docreg")
	// An explicit driver enables alpha vector search.
	t.Setenv("KNOWLEDGE_DRIVER", "pgvector")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Knowledge.Driver != "pgvector" {
		t.Errorf("explicit KNOWLEDGE_DRIVER ignored: got %q", cfg.Knowledge.Driver)
	}
}

func TestLoadRejectsNonPositiveContentLimits(t *testing.T) {
	requirePostgresDSN(t)
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "governance files", key: "GOVERNANCE_UPLOAD_MAX_BYTES"},
		{name: "knowledge", key: "KNOWLEDGE_MAX_FILE_BYTES"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, "0")
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s=0", tc.key)
			}
		})
	}
}
