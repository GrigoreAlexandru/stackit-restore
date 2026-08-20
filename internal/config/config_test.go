package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults_NoEnv(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "")
	t.Setenv("STACKIT_REGION", "")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "")
	t.Setenv("LOCAL_HOST", "")
	t.Setenv("LOCAL_PORT", "")
	t.Setenv("LOCAL_DB", "")
	t.Setenv("LOCAL_DATABASE", "")
	t.Setenv("LOCAL_USER", "")
	t.Setenv("LOCAL_PASS", "")
	t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "")
	t.Setenv("STACKIT_OPERATION_TIMEOUT_SECONDS", "")
	t.Setenv("POSTGRES_DUMP_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error with no env: %v", err)
	}

	if cfg.ProjectID != "" {
		t.Fatalf("expected empty project id, got %q", cfg.ProjectID)
	}
	if cfg.Region != "" {
		t.Fatalf("expected empty region, got %q", cfg.Region)
	}
	if cfg.ServiceAccountKeyPath != "" {
		t.Fatalf("expected empty sa key path, got %q", cfg.ServiceAccountKeyPath)
	}
	if cfg.LocalHost != "localhost" {
		t.Fatalf("expected default local host localhost, got %q", cfg.LocalHost)
	}
	if cfg.LocalPort != 5432 {
		t.Fatalf("expected default local port 5432, got %d", cfg.LocalPort)
	}
	if cfg.LocalDB != "postgres" {
		t.Fatalf("expected default local db postgres, got %q", cfg.LocalDB)
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

func TestLoad_NoDirCreationSideEffect(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentDir := filepath.Join(tempDir, "dumps_not_created_by_load")
	t.Setenv("POSTGRES_DUMP_DIR", nonExistentDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.DumpDir != nonExistentDir {
		t.Fatalf("expected dump dir %q, got %q", nonExistentDir, cfg.DumpDir)
	}

	if _, err := os.Stat(nonExistentDir); !os.IsNotExist(err) {
		t.Fatalf("expected directory %q NOT to be created by Load(), but it exists", nonExistentDir)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("STACKIT_PROJECT_ID", "from-env")
	t.Setenv("STACKIT_REGION", "eu03")
	t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "/etc/stackit/key.json")
	t.Setenv("LOCAL_HOST", "192.168.1.50")
	t.Setenv("LOCAL_PORT", "5433")
	t.Setenv("LOCAL_DB", "app_local_custom")
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
	if cfg.LocalDB != "app_local_custom" {
		t.Fatalf("expected local db app_local_custom, got %q", cfg.LocalDB)
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

func TestLoad_InvalidNumericValues(t *testing.T) {
	t.Run("invalid poll interval format", func(t *testing.T) {
		t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "not-a-number")
		_, err := Load()
		if err == nil {
			t.Fatal("expected error parsing non-numeric poll interval")
		}
	})

	t.Run("invalid timeout format", func(t *testing.T) {
		t.Setenv("STACKIT_OPERATION_TIMEOUT_SECONDS", "invalid")
		_, err := Load()
		if err == nil {
			t.Fatal("expected error parsing non-numeric timeout")
		}
	})

	t.Run("poll interval <= 0 rejected by ValidateLocal", func(t *testing.T) {
		t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "0")
		_, err := Load()
		if err == nil {
			t.Fatal("expected error when poll interval is 0")
		}
	})
}

func TestValidateLocal(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid default",
			cfg:     Default(),
			wantErr: false,
		},
		{
			name: "valid custom with no cloud auth",
			cfg: Config{
				OperationPollIntervalSeconds: 5,
				OperationTimeoutSeconds:      120,
				DumpDir:                      "/var/dumps",
			},
			wantErr: false,
		},
		{
			name: "invalid poll interval zero",
			cfg: Config{
				OperationPollIntervalSeconds: 0,
				OperationTimeoutSeconds:      100,
				DumpDir:                      "dumps",
			},
			wantErr: true,
		},
		{
			name: "invalid poll interval negative",
			cfg: Config{
				OperationPollIntervalSeconds: -5,
				OperationTimeoutSeconds:      100,
				DumpDir:                      "dumps",
			},
			wantErr: true,
		},
		{
			name: "invalid timeout zero",
			cfg: Config{
				OperationPollIntervalSeconds: 10,
				OperationTimeoutSeconds:      0,
				DumpDir:                      "dumps",
			},
			wantErr: true,
		},
		{
			name: "empty dump dir",
			cfg: Config{
				OperationPollIntervalSeconds: 10,
				OperationTimeoutSeconds:      100,
				DumpDir:                      "",
			},
			wantErr: true,
		},
		{
			name: "whitespace dump dir",
			cfg: Config{
				OperationPollIntervalSeconds: 10,
				OperationTimeoutSeconds:      100,
				DumpDir:                      "   ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateLocal()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLocal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateStackIT(t *testing.T) {
	validBase := Config{
		ProjectID:                    "proj-123",
		Region:                       "eu01",
		ServiceAccountKeyPath:        "/path/to/key.json",
		OperationPollIntervalSeconds: 10,
		OperationTimeoutSeconds:      600,
		DumpDir:                      "dumps",
	}

	tests := []struct {
		name    string
		modify  func(c *Config)
		wantErr bool
	}{
		{
			name:    "valid full config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "missing project id",
			modify: func(c *Config) {
				c.ProjectID = ""
			},
			wantErr: true,
		},
		{
			name: "whitespace project id",
			modify: func(c *Config) {
				c.ProjectID = "  "
			},
			wantErr: true,
		},
		{
			name: "missing region",
			modify: func(c *Config) {
				c.Region = ""
			},
			wantErr: true,
		},
		{
			name: "missing sa key path",
			modify: func(c *Config) {
				c.ServiceAccountKeyPath = ""
			},
			wantErr: true,
		},
		{
			name: "fails local validation first (invalid poll interval)",
			modify: func(c *Config) {
				c.OperationPollIntervalSeconds = 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validBase
			tt.modify(&c)
			err := c.ValidateStackIT()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStackIT() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHasAuth(t *testing.T) {
	tests := []struct {
		name     string
		keyPath  string
		wantAuth bool
	}{
		{"valid key path", "/path/to/key.json", true},
		{"empty key path", "", false},
		{"whitespace key path", "   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{ServiceAccountKeyPath: tt.keyPath}
			if got := c.HasAuth(); got != tt.wantAuth {
				t.Errorf("HasAuth() = %v, want %v", got, tt.wantAuth)
			}
		})
	}
}
