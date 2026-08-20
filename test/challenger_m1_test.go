package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

// threadSafeBuffer wraps bytes.Buffer with a mutex for safe concurrent testing of command outputs
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *threadSafeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *threadSafeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// -----------------------------------------------------------------------------
// 1. WriteErrorLog Adversarial Verification Tests
// -----------------------------------------------------------------------------

func TestWriteErrorLog_NilLogger(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	details := map[string]string{
		"Instance": "Prod-DB",
		"Database": "orders",
	}

	logPath, err := postgres.WriteErrorLog(nil, dumpDir, "restore", details, errors.New("connection failed"))
	if err != nil {
		t.Fatalf("WriteErrorLog with nil logger failed: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read error log: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "(No stdout/stderr captured during this session)") {
		t.Errorf("expected fallback message for nil logger, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "Instance:    Prod-DB") {
		t.Errorf("expected details in log file, got:\n%s", contentStr)
	}
}

func TestWriteErrorLog_EmptyLogger(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	logger := postgres.NewExecutionLogger(nil)
	// Nothing appended

	logPath, err := postgres.WriteErrorLog(logger, dumpDir, "dump", nil, errors.New("timeout"))
	if err != nil {
		t.Fatalf("WriteErrorLog with empty logger failed: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read error log: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "(No stdout/stderr captured during this session)") {
		t.Errorf("expected fallback message for empty logger, got:\n%s", contentStr)
	}
}

func TestWriteErrorLog_PopulatedAndLargeLogBuffers(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	logger := postgres.NewExecutionLogger(nil)

	// Generate 10MB of structured log lines
	const targetSize = 10 * 1024 * 1024 // 10 MB
	chunk := []byte("pg_restore: processing data for table \"users\" (chunk 1234567890)...\n")
	written := 0
	for written < targetSize {
		logger.Append(chunk)
		written += len(chunk)
	}

	details := map[string]string{
		"Batch": "LargeLogTest",
		"Size":  fmt.Sprintf("%d bytes", written),
	}

	logPath, err := postgres.WriteErrorLog(logger, dumpDir, "large_restore", details, errors.New("oom or interrupted"))
	if err != nil {
		t.Fatalf("WriteErrorLog with 10MB buffer failed: %v", err)
	}

	stat, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("failed to stat generated log: %v", err)
	}

	if stat.Size() < int64(targetSize) {
		t.Fatalf("expected log file size >= %d, got %d", targetSize, stat.Size())
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read generated log: %v", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "LargeLogTest") {
		t.Errorf("expected details to be present in log")
	}
	if !strings.Contains(contentStr, "pg_restore: processing data for table \"users\"") {
		t.Errorf("expected captured log contents to be present in log")
	}
}

func TestWriteErrorLog_SpecialCharactersAndNoTrailingNewline(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	logger := postgres.NewExecutionLogger(nil)
	specialLog := "Special Unicode: 🚀 🔥 — \x00\x01\x02 test null bytes without newline"
	logger.Append([]byte(specialLog))

	details := map[string]string{
		"UnicodeKey_🔥": "UnicodeVal_🚀",
		"EmptyKey":      "",
	}

	logPath, err := postgres.WriteErrorLog(logger, dumpDir, "  ACTION_WITH_SPACES  ", details, nil)
	if err != nil {
		t.Fatalf("WriteErrorLog failed: %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, specialLog) {
		t.Errorf("expected special unicode log to be preserved verbatim")
	}
	if !strings.Contains(contentStr, "UnicodeKey_🔥:") {
		t.Errorf("expected unicode key in details")
	}
}

func TestWriteErrorLog_ConcurrentWriteAndAppend(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	logger := postgres.NewExecutionLogger(nil)
	var wg sync.WaitGroup

	stopAppend := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopAppend:
				return
			default:
				logger.Append([]byte("concurrent append data line\n"))
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	const numWriters = 10
	wg.Add(numWriters)
	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()
			_, err := postgres.WriteErrorLog(
				logger,
				dumpDir,
				fmt.Sprintf("action_%d", id),
				map[string]string{"worker": fmt.Sprintf("%d", id)},
				errors.New("concurrent error"),
			)
			if err != nil {
				t.Errorf("concurrent WriteErrorLog %d failed: %v", id, err)
			}
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(stopAppend)
	wg.Wait()
}

// -----------------------------------------------------------------------------
// 2. RunPgRestore Warning Detection & Exit Handling Adversarial Tests
// -----------------------------------------------------------------------------

func createMockBinary(t *testing.T, binName string, stdout, stderr string, exitCode int) string {
	tempDir := t.TempDir()
	binPath := filepath.Join(tempDir, binName)
	script := fmt.Sprintf("#!/bin/sh\ncat << 'MOCK_EOF_STDOUT' >&1\n%s\nMOCK_EOF_STDOUT\ncat << 'MOCK_EOF_STDERR' >&2\n%s\nMOCK_EOF_STDERR\nexit %d\n",
		stdout, stderr, exitCode)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock binary %s: %v", binName, err)
	}
	return tempDir
}

func TestRunPgRestore_MockWarningDetection(t *testing.T) {
	tests := []struct {
		name          string
		stdout        string
		stderr        string
		exitCode      int
		wantErrIs     error
		wantFatal     bool
		wantLogNotice bool
	}{
		{
			name:      "Exit 0 - Clean Success",
			stdout:    "pg_restore: processing data...\npg_restore: finished successfully",
			stderr:    "",
			exitCode:  0,
			wantErrIs: nil,
			wantFatal: false,
		},
		{
			name:          "Exit 1 - Missing extension warning",
			stdout:        "pg_restore: creating EXTENSION \"pg_stat_kcache\"",
			stderr:        "pg_restore: error: could not execute query: ERROR: extension \"pg_stat_kcache\" is not available\npg_restore: warning: errors were ignored during processing",
			exitCode:      1,
			wantErrIs:     postgres.ErrRestoreWithWarnings,
			wantFatal:     false,
			wantLogNotice: true,
		},
		{
			name:          "Exit 1 - Already exists warning",
			stdout:        "pg_restore: creating TABLE \"users\"",
			stderr:        "pg_restore: error: could not execute query: ERROR: relation \"users\" already exists\npg_restore: warning: errors were ignored during processing",
			exitCode:      1,
			wantErrIs:     postgres.ErrRestoreWithWarnings,
			wantFatal:     false,
			wantLogNotice: true,
		},
		{
			name:          "Exit 1 - Permission denied to create extension",
			stdout:        "pg_restore: creating EXTENSION \"pg_stat_statements\"",
			stderr:        "ERROR: permission denied to create extension \"pg_stat_statements\"\nHINT: Must be superuser.",
			exitCode:      1,
			wantErrIs:     postgres.ErrRestoreWithWarnings,
			wantFatal:     false,
			wantLogNotice: true,
		},
		{
			name:      "Exit 1 - Fatal connection refused",
			stdout:    "",
			stderr:    "pg_restore: error: connection to server at \"127.0.0.1\", port 5432 failed: Connection refused\npg_restore: error: could not connect to server",
			exitCode:  1,
			wantErrIs: nil,
			wantFatal: true,
		},
		{
			name:      "Exit 1 - Fatal password authentication failed",
			stdout:    "",
			stderr:    "FATAL: password authentication failed for user \"postgres\"",
			exitCode:  1,
			wantErrIs: nil,
			wantFatal: true,
		},
		{
			name:      "Exit 1 - Mixed fatal keyword with warning keyword (Fatal takes precedence)",
			stdout:    "pg_restore: creating EXTENSION \"pg_stat_kcache\"",
			stderr:    "extension \"pg_stat_kcache\" is not available\nFATAL: password authentication failed for user \"postgres\"",
			exitCode:  1,
			wantErrIs: nil,
			wantFatal: true,
		},
		{
			name:      "Exit 2 - Fatal syntax / command error",
			stdout:    "",
			stderr:    "pg_restore: invalid option --x",
			exitCode:  2,
			wantErrIs: nil,
			wantFatal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := createMockBinary(t, "pg_restore", tt.stdout, tt.stderr, tt.exitCode)

			origPath := os.Getenv("PATH")
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

			var outBuf threadSafeBuffer
			logger := postgres.NewExecutionLogger(&outBuf)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			dumpFile := filepath.Join(t.TempDir(), "test.dump")
			if err := os.WriteFile(dumpFile, []byte("mock"), 0644); err != nil {
				t.Fatalf("failed to create dump file: %v", err)
			}

			err := postgres.RunPgRestore(
				ctx,
				"127.0.0.1",
				5432,
				"testdb",
				dumpFile,
				postgres.Credentials{User: "u", Password: "p", SSLMode: "disable"},
				logger,
			)

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("expected error %v, got %v", tt.wantErrIs, err)
				}
				if tt.wantLogNotice {
					logStr := logger.GetLog()
					if !strings.Contains(logStr, "pg_restore completed with non-fatal extension/role warnings") {
						t.Errorf("expected notice in logger transcript, got: %q", logStr)
					}
					if !strings.Contains(outBuf.String(), "pg_restore completed with non-fatal extension/role warnings") {
						t.Errorf("expected notice in logger writer output, got: %q", outBuf.String())
					}
				}
			} else if tt.wantFatal {
				if err == nil {
					t.Fatalf("expected fatal error, got nil")
				}
				if errors.Is(err, postgres.ErrRestoreWithWarnings) {
					t.Fatalf("did NOT expect ErrRestoreWithWarnings for fatal error: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected nil error on clean exit, got: %v", err)
				}
			}
		})
	}
}

func TestRunPgRestore_NilLoggerWithWarnings(t *testing.T) {
	binDir := createMockBinary(t, "pg_restore", "", "extension \"pg_stat_kcache\" is not available", 1)

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	dumpFile := filepath.Join(t.TempDir(), "test.dump")
	if err := os.WriteFile(dumpFile, []byte("mock"), 0644); err != nil {
		t.Fatalf("failed to create dump file: %v", err)
	}

	err := postgres.RunPgRestore(
		context.Background(),
		"127.0.0.1",
		5432,
		"testdb",
		dumpFile,
		postgres.Credentials{User: "u", Password: "p", SSLMode: "disable"},
		nil,
	)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, postgres.ErrRestoreWithWarnings) {
		t.Errorf("did not expect ErrRestoreWithWarnings with nil logger")
	}
}

func TestRunPgRestore_EmptyDumpPathValidation(t *testing.T) {
	err := postgres.RunPgRestore(
		context.Background(),
		"localhost",
		5432,
		"db",
		"   ",
		postgres.Credentials{},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for whitespace dump path")
	}
	if !strings.Contains(err.Error(), "dump file path is empty") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// -----------------------------------------------------------------------------
// 3. Config Load & Validation Adversarial Tests
// -----------------------------------------------------------------------------

func TestConfigLoad_DotEnvParsing(t *testing.T) {
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	dotEnvContent := `
# Configuration File
STACKIT_PROJECT_ID="proj-env-123"
STACKIT_REGION='eu02'
STACKIT_SERVICE_ACCOUNT_KEY_PATH = /keys/sa.json
LOCAL_HOST=10.0.0.1
LOCAL_PORT=5434
LOCAL_DB="test_db"
LOCAL_USER=myuser
LOCAL_PASS="mypass#with#hashes"
STACKIT_OPERATION_POLL_INTERVAL_SECONDS=15
STACKIT_OPERATION_TIMEOUT_SECONDS=450
POSTGRES_DUMP_DIR=/tmp/custom_dumps

# Blank line and invalid line
INVALID_LINE_NO_EQUALS
`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotEnvContent), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	for _, env := range []string{
		"STACKIT_PROJECT_ID", "STACKIT_REGION", "STACKIT_SERVICE_ACCOUNT_KEY_PATH",
		"LOCAL_HOST", "LOCAL_PORT", "LOCAL_DB", "LOCAL_DATABASE", "LOCAL_USER", "LOCAL_PASS",
		"STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "STACKIT_OPERATION_TIMEOUT_SECONDS", "POSTGRES_DUMP_DIR",
	} {
		t.Setenv(env, "")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed with valid .env: %v", err)
	}

	if cfg.ProjectID != "proj-env-123" {
		t.Errorf("ProjectID = %q, want %q", cfg.ProjectID, "proj-env-123")
	}
	if cfg.Region != "eu02" {
		t.Errorf("Region = %q, want %q", cfg.Region, "eu02")
	}
	if cfg.ServiceAccountKeyPath != "/keys/sa.json" {
		t.Errorf("ServiceAccountKeyPath = %q, want %q", cfg.ServiceAccountKeyPath, "/keys/sa.json")
	}
	if cfg.LocalHost != "10.0.0.1" || cfg.LocalPort != 5434 || cfg.LocalDB != "test_db" {
		t.Errorf("Local DB config mismatch: %+v", cfg)
	}
	if cfg.LocalPass != "mypass#with#hashes" {
		t.Errorf("LocalPass = %q, want %q", cfg.LocalPass, "mypass#with#hashes")
	}
	if cfg.OperationPollIntervalSeconds != 15 {
		t.Errorf("OperationPollIntervalSeconds = %d, want 15", cfg.OperationPollIntervalSeconds)
	}
	if cfg.OperationTimeoutSeconds != 450 {
		t.Errorf("OperationTimeoutSeconds = %d, want 450", cfg.OperationTimeoutSeconds)
	}
	if cfg.DumpDir != "/tmp/custom_dumps" {
		t.Errorf("DumpDir = %q, want /tmp/custom_dumps", cfg.DumpDir)
	}
}

func TestConfigLoad_PreExistingEnvNotOverwrittenByDotEnv(t *testing.T) {
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	dotEnvContent := `STACKIT_PROJECT_ID=from_dotenv`
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotEnvContent), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("STACKIT_PROJECT_ID", "pre_existing_env")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	if cfg.ProjectID != "pre_existing_env" {
		t.Fatalf("expected pre-existing env var to take precedence over .env, got %q", cfg.ProjectID)
	}
}

func TestConfigLoad_MissingDotEnvLoadsDefaults(t *testing.T) {
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	for _, env := range []string{
		"STACKIT_PROJECT_ID", "STACKIT_REGION", "STACKIT_SERVICE_ACCOUNT_KEY_PATH",
		"LOCAL_HOST", "LOCAL_PORT", "LOCAL_DB", "LOCAL_DATABASE", "LOCAL_USER", "LOCAL_PASS",
		"STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "STACKIT_OPERATION_TIMEOUT_SECONDS", "POSTGRES_DUMP_DIR",
	} {
		t.Setenv(env, "")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load without .env failed: %v", err)
	}

	if cfg.DumpDir != "dumps" || cfg.LocalPort != 5432 || cfg.LocalHost != "localhost" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestConfigLoad_NoFilesystemSideEffects(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentNestedDir := filepath.Join(tempDir, "deeply", "nested", "dump", "dir")
	t.Setenv("POSTGRES_DUMP_DIR", nonExistentNestedDir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}

	if cfg.DumpDir != nonExistentNestedDir {
		t.Errorf("DumpDir = %q, want %q", cfg.DumpDir, nonExistentNestedDir)
	}

	if _, err := os.Stat(nonExistentNestedDir); !os.IsNotExist(err) {
		t.Errorf("expected %q NOT to exist, but it was created as a side effect!", nonExistentNestedDir)
	}

	parent := filepath.Join(tempDir, "deeply")
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Errorf("expected %q NOT to exist!", parent)
	}
}

func TestConfigLoad_InvalidNumericalInputs(t *testing.T) {
	invalidCases := []struct {
		name    string
		envKey  string
		envVal  string
		wantErr bool
	}{
		{"Poll Interval non-numeric", "STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "abc", true},
		{"Poll Interval float", "STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "10.5", true},
		{"Poll Interval negative", "STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "-5", true},
		{"Poll Interval zero", "STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "0", true},
		{"Timeout non-numeric", "STACKIT_OPERATION_TIMEOUT_SECONDS", "XYZ", true},
		{"Timeout float", "STACKIT_OPERATION_TIMEOUT_SECONDS", "600.0", true},
		{"Timeout negative", "STACKIT_OPERATION_TIMEOUT_SECONDS", "-1", true},
		{"Timeout zero", "STACKIT_OPERATION_TIMEOUT_SECONDS", "0", true},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS", "10")
			t.Setenv("STACKIT_OPERATION_TIMEOUT_SECONDS", "600")
			t.Setenv(tc.envKey, tc.envVal)

			_, err := config.Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("config.Load() with %s=%q error = %v, wantErr %v", tc.envKey, tc.envVal, err, tc.wantErr)
			}
		})
	}
}

func TestConfigLoad_LocalPortHandling(t *testing.T) {
	t.Run("invalid local port retains default", func(t *testing.T) {
		t.Setenv("LOCAL_PORT", "invalid_port")
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.LocalPort != 5432 {
			t.Errorf("expected default port 5432, got %d", cfg.LocalPort)
		}
	})

	t.Run("negative local port retains default", func(t *testing.T) {
		t.Setenv("LOCAL_PORT", "-5432")
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.LocalPort != 5432 {
			t.Errorf("expected default port 5432, got %d", cfg.LocalPort)
		}
	})

	t.Run("valid custom local port", func(t *testing.T) {
		t.Setenv("LOCAL_PORT", "5439")
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.LocalPort != 5439 {
			t.Errorf("expected port 5439, got %d", cfg.LocalPort)
		}
	})
}

// -----------------------------------------------------------------------------
// 4. Stackit IsDeleteForbidden Adversarial Verification Tests
// -----------------------------------------------------------------------------

type customErrorWithStatus struct {
	StatusCode int
	msg        string
}

func (c *customErrorWithStatus) Error() string {
	return fmt.Sprintf("custom error (%d): %s", c.StatusCode, c.msg)
}

func TestStackitIsDeleteForbidden_Adversarial(t *testing.T) {
	t.Run("direct sentinel", func(t *testing.T) {
		if !stackit.IsDeleteForbidden(stackit.ErrDeleteInstanceForbidden) {
			t.Errorf("expected direct ErrDeleteInstanceForbidden to return true")
		}
	})

	t.Run("single wrapped sentinel", func(t *testing.T) {
		err := fmt.Errorf("outer wrapper: %w", stackit.ErrDeleteInstanceForbidden)
		if !stackit.IsDeleteForbidden(err) {
			t.Errorf("expected wrapped ErrDeleteInstanceForbidden to return true")
		}
	})

	t.Run("deeply chained wrapped sentinel", func(t *testing.T) {
		err := fmt.Errorf("level 1: %w",
			fmt.Errorf("level 2: %w",
				fmt.Errorf("level 3: %w",
					fmt.Errorf("level 4: %w", stackit.ErrDeleteInstanceForbidden))))
		if !stackit.IsDeleteForbidden(err) {
			t.Errorf("expected deeply chained ErrDeleteInstanceForbidden to return true")
		}
	})

	t.Run("joined error containing sentinel", func(t *testing.T) {
		err := errors.Join(
			errors.New("unrelated error"),
			stackit.ErrDeleteInstanceForbidden,
			errors.New("another error"),
		)
		if !stackit.IsDeleteForbidden(err) {
			t.Errorf("expected joined error containing sentinel to return true")
		}
	})

	t.Run("direct OpenAPI 403", func(t *testing.T) {
		oapi403 := oapierror.NewError(403, "Forbidden")
		if !stackit.IsDeleteForbidden(oapi403) {
			t.Errorf("expected direct OpenAPI 403 to return true")
		}
	})

	t.Run("deeply chained OpenAPI 403", func(t *testing.T) {
		oapi403 := oapierror.NewError(403, "Forbidden")
		err := fmt.Errorf("w1: %w", fmt.Errorf("w2: %w", fmt.Errorf("w3: %w", oapi403)))
		if !stackit.IsDeleteForbidden(err) {
			t.Errorf("expected deeply wrapped OpenAPI 403 to return true")
		}
	})

	t.Run("joined error containing OpenAPI 403", func(t *testing.T) {
		oapi403 := oapierror.NewError(403, "Forbidden")
		err := errors.Join(errors.New("io error"), oapi403)
		if !stackit.IsDeleteForbidden(err) {
			t.Errorf("expected joined error with OpenAPI 403 to return true")
		}
	})

	t.Run("raw string containing 403 is rejected", func(t *testing.T) {
		rawErrors := []error{
			errors.New("403 Forbidden"),
			errors.New("HTTP 403: user lacks permissions"),
			errors.New("error code: 403"),
			fmt.Errorf("status 403 occurred during delete: %s", "details"),
			errors.New("forbidden action"),
			errors.New("Permission denied (403)"),
		}
		for _, rawErr := range rawErrors {
			if stackit.IsDeleteForbidden(rawErr) {
				t.Errorf("expected raw string error %q NOT to be recognized as forbidden", rawErr.Error())
			}
		}
	})

	t.Run("custom struct with StatusCode 403 is rejected", func(t *testing.T) {
		customErr := &customErrorWithStatus{StatusCode: 403, msg: "forbidden custom"}
		if stackit.IsDeleteForbidden(customErr) {
			t.Errorf("expected non-OpenAPI custom struct NOT to be recognized as forbidden")
		}
		wrappedCustom := fmt.Errorf("wrapped: %w", customErr)
		if stackit.IsDeleteForbidden(wrappedCustom) {
			t.Errorf("expected wrapped non-OpenAPI custom struct NOT to be recognized as forbidden")
		}
	})

	t.Run("OpenAPI errors with other status codes return false", func(t *testing.T) {
		statusCodes := []int{200, 400, 401, 404, 409, 429, 500, 502, 503}
		for _, code := range statusCodes {
			oapiErr := oapierror.NewError(code, fmt.Sprintf("Status %d", code))
			if stackit.IsDeleteForbidden(oapiErr) {
				t.Errorf("expected OpenAPI error with code %d NOT to return true", code)
			}
			wrapped := fmt.Errorf("wrapped: %w", oapiErr)
			if stackit.IsDeleteForbidden(wrapped) {
				t.Errorf("expected wrapped OpenAPI error with code %d NOT to return true", code)
			}
		}
	})

	t.Run("nil error returns false", func(t *testing.T) {
		if stackit.IsDeleteForbidden(nil) {
			t.Errorf("expected nil error to return false")
		}
	})
}
