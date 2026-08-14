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

func TestLoadReadsKeyFlowEnvironmentVariables(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "from-env")
	t.Setenv("STACKIT_REGION", "eu03")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "/etc/stackit/key.json")
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

func TestLoadRejectsMissingRequiredFields(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "")
	t.Setenv("STACKIT_REGION", "")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for missing required fields")
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
