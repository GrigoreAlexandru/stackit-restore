package config

import (
	"testing"
)

func TestLoadUsesDefaultsAndValidatesRequiredValues(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "proj-123")
	t.Setenv("STACKIT_REGION", "eu01")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "/path/to/sa-key.json")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ProjectID != "proj-123" {
		t.Fatalf("expected project id proj-123, got %q", cfg.ProjectID)
	}
	if cfg.Region != "eu01" {
		t.Fatalf("expected region eu01, got %q", cfg.Region)
	}
	if cfg.ServiceAccountKeyPath != "/path/to/sa-key.json" {
		t.Fatalf("expected service account key path /path/to/sa-key.json, got %q", cfg.ServiceAccountKeyPath)
	}
	if cfg.LocalHost != "localhost" {
		t.Fatalf("expected default local host localhost, got %q", cfg.LocalHost)
	}
	if cfg.LocalPort != 5432 {
		t.Fatalf("expected default local port 5432, got %d", cfg.LocalPort)
	}
	if cfg.OperationPollIntervalSeconds != 10 {
		t.Fatalf("expected default poll interval 10, got %d", cfg.OperationPollIntervalSeconds)
	}
	if cfg.OperationTimeoutSeconds != 600 {
		t.Fatalf("expected default timeout 600, got %d", cfg.OperationTimeoutSeconds)
	}
	if cfg.DumpDir != "dumps" {
		t.Fatalf("expected default dump dir dumps, got %q", cfg.DumpDir)
	}
}

func TestLoadReadsKeyFlowAndLocalEnvironmentVariables(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "from-env")
	t.Setenv("STACKIT_REGION", "eu03")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "/etc/stackit/key.json")
	t.Setenv("LOCAL_HOST", "192.168.1.50")
	t.Setenv("LOCAL_PORT", "5433")
	t.Setenv("LOCAL_USER", "postgres_local")
	t.Setenv("LOCAL_PASS", "postgres_secret")
	t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "25")
	t.Setenv("STACKIT_OPERATION_TIMEOUT_SECONDS", "900")
	t.Setenv("POSTGRES_DUMP_DIR", "/tmp/custom-dump-dir")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.ProjectID != "from-env" {
		t.Fatalf("expected env override project id, got %q", cfg.ProjectID)
	}
	if cfg.Region != "eu03" {
		t.Fatalf("expected env override region, got %q", cfg.Region)
	}
	if cfg.ServiceAccountKeyPath != "/etc/stackit/key.json" {
		t.Fatalf("expected sa key path from env, got %q", cfg.ServiceAccountKeyPath)
	}
	if cfg.LocalHost != "192.168.1.50" || cfg.LocalPort != 5433 {
		t.Fatalf("expected local endpoint 192.168.1.50:5433, got %s:%d", cfg.LocalHost, cfg.LocalPort)
	}
	if cfg.LocalUser != "postgres_local" || cfg.LocalPass != "postgres_secret" {
		t.Fatalf("expected local user/pass from env, got %s / %s", cfg.LocalUser, cfg.LocalPass)
	}
	if cfg.OperationPollIntervalSeconds != 25 {
		t.Fatalf("expected poll interval from env 25, got %d", cfg.OperationPollIntervalSeconds)
	}
	if cfg.OperationTimeoutSeconds != 900 {
		t.Fatalf("expected timeout from env 900, got %d", cfg.OperationTimeoutSeconds)
	}
	if cfg.DumpDir != "/tmp/custom-dump-dir" {
		t.Fatalf("expected dump dir from env, got %q", cfg.DumpDir)
	}
}

func TestLoadRejectsMissingKeyPath(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "proj-123")
	t.Setenv("STACKIT_REGION", "eu01")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error when STACKIT_SERVICE_ACCOUNT_KEY_PATH is missing")
	}
}

func TestLoadRejectsInvalidIntervalValues(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "proj-123")
	t.Setenv("STACKIT_REGION", "eu01")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "/path/to/key.json")
	t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for invalid poll interval")
	}
}
