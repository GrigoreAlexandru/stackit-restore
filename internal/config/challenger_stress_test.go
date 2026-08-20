package config

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestConfig_Load_StrictZeroFileSystemMutation verifies that Load() never creates any
// directory or file under any circumstance, across multiple invocations and parallel threads.
func TestConfig_Load_StrictZeroFileSystemMutation(t *testing.T) {
	tempDir := t.TempDir()
	canaryDir1 := filepath.Join(tempDir, "dumps_canary_single")
	canaryDir2 := filepath.Join(tempDir, "deeply", "nested", "canary", "path")

	// Set dump dir to non-existent path
	t.Setenv("POSTGRES_DUMP_DIR", canaryDir1)
	t.Setenv("STACKIT_PROJECT_ID", "test-project")
	t.Setenv("STACKIT_REGION", "eu01")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.DumpDir != canaryDir1 {
		t.Fatalf("cfg.DumpDir = %q, want %q", cfg.DumpDir, canaryDir1)
	}

	if _, err := os.Stat(canaryDir1); !os.IsNotExist(err) {
		t.Fatalf("VIOLATION: Load() created directory on filesystem: %s", canaryDir1)
	}

	// Concurrent Load() invocations with deeply nested path
	t.Setenv("POSTGRES_DUMP_DIR", canaryDir2)
	const concurrency = 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			c, err := Load()
			if err != nil {
				t.Errorf("concurrent Load() error: %v", err)
			}
			if c.DumpDir != canaryDir2 {
				t.Errorf("unexpected DumpDir: %s", c.DumpDir)
			}
		}()
	}
	wg.Wait()

	if _, err := os.Stat(canaryDir2); !os.IsNotExist(err) {
		t.Fatalf("VIOLATION: Concurrent Load() created directory on filesystem: %s", canaryDir2)
	}
}

// TestConfig_ValidateMatrix tests exhaustive permutations of valid, empty, whitespace,
// negative, and boundary values for ValidateLocal and ValidateStackIT.
func TestConfig_ValidateMatrix(t *testing.T) {
	whitespaceSamples := []string{
		"",
		" ",
		"   ",
		"\t",
		"\n",
		"\r\n",
		" \t \n \r ",
		"\u00A0", // non-breaking space
		"\u2000", // en quad
	}

	t.Run("ValidateLocal whitespace dump dirs must all fail", func(t *testing.T) {
		for i, ws := range whitespaceSamples {
			cfg := Default()
			cfg.DumpDir = ws
			err := cfg.ValidateLocal()
			if err == nil {
				t.Errorf("sample %d (%q): ValidateLocal() succeeded unexpectedly for whitespace DumpDir", i, ws)
			}
		}
	})

	t.Run("ValidateLocal negative or zero intervals and timeouts", func(t *testing.T) {
		badIntervals := []int{0, -1, -500, math.MinInt32, math.MinInt}
		for _, interval := range badIntervals {
			cfg := Default()
			cfg.OperationPollIntervalSeconds = interval
			if err := cfg.ValidateLocal(); err == nil {
				t.Errorf("expected error for interval=%d, got nil", interval)
			}
		}

		badTimeouts := []int{0, -1, -600, math.MinInt32, math.MinInt}
		for _, timeout := range badTimeouts {
			cfg := Default()
			cfg.OperationTimeoutSeconds = timeout
			if err := cfg.ValidateLocal(); err == nil {
				t.Errorf("expected error for timeout=%d, got nil", timeout)
			}
		}
	})

	t.Run("ValidateLocal extreme positive values succeed", func(t *testing.T) {
		cfg := Default()
		cfg.OperationPollIntervalSeconds = math.MaxInt32
		cfg.OperationTimeoutSeconds = math.MaxInt32
		cfg.DumpDir = "/extreme/path/dumps"
		if err := cfg.ValidateLocal(); err != nil {
			t.Errorf("unexpected failure for large valid bounds: %v", err)
		}
	})

	t.Run("ValidateStackIT requires all 3 cloud fields independently", func(t *testing.T) {
		validBase := Config{
			ProjectID:                    "proj-abc-123",
			Region:                       "eu01",
			ServiceAccountKeyPath:        "/var/run/secret.json",
			OperationPollIntervalSeconds: 10,
			OperationTimeoutSeconds:      600,
			DumpDir:                      "dumps",
		}

		if err := validBase.ValidateStackIT(); err != nil {
			t.Fatalf("validBase failed ValidateStackIT: %v", err)
		}

		// Test each cloud field with whitespace/empty
		for _, ws := range []string{"", "  ", "\t\n"} {
			// ProjectID
			c1 := validBase
			c1.ProjectID = ws
			if err := c1.ValidateStackIT(); err == nil {
				t.Errorf("ValidateStackIT succeeded with bad ProjectID %q", ws)
			}

			// Region
			c2 := validBase
			c2.Region = ws
			if err := c2.ValidateStackIT(); err == nil {
				t.Errorf("ValidateStackIT succeeded with bad Region %q", ws)
			}

			// ServiceAccountKeyPath
			c3 := validBase
			c3.ServiceAccountKeyPath = ws
			if err := c3.ValidateStackIT(); err == nil {
				t.Errorf("ValidateStackIT succeeded with bad ServiceAccountKeyPath %q", ws)
			}
		}
	})
}

// TestConfig_Load_EnvironmentAdversarialInputs tests bad environment strings
func TestConfig_Load_EnvironmentAdversarialInputs(t *testing.T) {
	t.Run("extremely large int parse", func(t *testing.T) {
		t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "999999999999999999999999999999999999999")
		_, err := Load()
		if err == nil {
			t.Fatal("expected overflow error parsing huge interval")
		}
	})

	t.Run("empty environment with default fallback is valid for local operations", func(t *testing.T) {
		t.Setenv("STACKIT_PROJECT_ID", "")
		t.Setenv("STACKIT_REGION", "")
		t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "")
		t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "")
		t.Setenv("STACKIT_OPERATION_TIMEOUT_SECONDS", "")
		t.Setenv("POSTGRES_DUMP_DIR", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() failed with default fallback: %v", err)
		}

		// Local validation passes
		if err := cfg.ValidateLocal(); err != nil {
			t.Errorf("default cfg failed ValidateLocal: %v", err)
		}

		// Cloud validation fails (because cloud fields are empty)
		if err := cfg.ValidateStackIT(); err == nil {
			t.Error("default cfg should fail ValidateStackIT when cloud credentials missing")
		}
	})
}
