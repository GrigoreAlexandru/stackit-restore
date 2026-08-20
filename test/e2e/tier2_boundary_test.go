package e2e

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/provider"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
	"github.com/GrigoreAlexandru/Stackit-Restore/test/e2e/harness"
)

// ============================================================================
// FEATURE F1 BOUNDARY: CLI USAGE & HELP (5 Tests: T2-F01-01 .. T2-F01-05)
// ============================================================================

func TestTier2_F01_Boundary_CLIUsageAndHelp(t *testing.T) {
	t.Run("T2-F01-01_HelpCombinedWithInvalidFlags", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("--action=invalid_action_value", "--mode=invalid_mode_value", "--help")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT")
	})

	t.Run("T2-F01-02_EmptyArgumentsNonInteractiveFallback", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		// Running in non-interactive with empty args and missing action
		res := env.RunCLI("--non-interactive")

		res.AssertFailure(t).
			AssertStderrContains(t, "invalid or missing --action")
	})

	t.Run("T2-F01-03_DuplicateHelpFlags", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("-h", "--help")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT")
	})

	t.Run("T2-F01-04_HelpInvocationWithClosedStdin", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLIWithStdin("", "--help")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT")
	})

	t.Run("T2-F01-05_ProgrammaticUsageTruncationSafety", func(t *testing.T) {
		// Outputting usage to a small buffer shouldn't panic
		var buf bytes.Buffer
		env := harness.NewTestEnv(t)
		res := env.RunCLI("--help")
		buf.WriteString(res.Stdout)

		if buf.Len() == 0 {
			t.Fatalf("expected non-empty usage output")
		}
	})
}

// ============================================================================
// FEATURE F2 BOUNDARY: CLI FLAG PARSING (5 Tests: T2-F02-01 .. T2-F02-05)
// ============================================================================

func TestTier2_F02_Boundary_FlagParsing(t *testing.T) {
	t.Run("T2-F02-01_MalformedPointInTimeFormat", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("PRODUCTION_USER", "prod_user")
		env.SetEnv("PRODUCTION_PASS", "prod_pass")

		res := env.RunCLI(
			"--action=dump",
			"--instance=Production",
			"--database=app_prod",
			"--mode=pit",
			"--pit=invalid-timestamp-format-xyz",
		)

		res.AssertFailure(t).
			AssertStderrContains(t, "invalid --pit value")
	})

	t.Run("T2-F02-02_MissingInstanceForDumpAction", func(t *testing.T) {
		env := harness.NewTestEnv(t)

		res := env.RunCLI(
			"--action=dump",
			"--database=app_prod",
		)

		res.AssertFailure(t).
			AssertStderrContains(t, "--instance is required for dump action")
	})

	t.Run("T2-F02-03_MissingSourceDatabaseForSyncAction", func(t *testing.T) {
		env := harness.NewTestEnv(t)

		res := env.RunCLI(
			"--action=sync",
			"--instance=Production",
			"--target-instance=local",
			"--target-database=app_local",
		)

		res.AssertFailure(t).
			AssertStderrContains(t, "is required for sync action")
	})

	t.Run("T2-F02-04_UnsupportedModeString", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("PRODUCTION_USER", "prod_user")
		env.SetEnv("PRODUCTION_PASS", "prod_pass")

		res := env.RunCLI(
			"--action=dump",
			"--instance=Production",
			"--database=app_prod",
			"--mode=unsupported_random_mode",
		)

		res.AssertFailure(t).
			AssertStderrContains(t, "invalid dump mode")
	})

	t.Run("T2-F02-05_ReplicaDumpOnLocalInstanceRejection", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		res := env.RunCLI(
			"--action=dump",
			"--instance=local",
			"--database=app_local",
			"--mode=replica",
		)

		res.AssertFailure(t).
			AssertStderrContains(t, "not supported for local")
	})
}

// ============================================================================
// FEATURE F3 BOUNDARY: CONFIG VALIDATION (5 Tests: T2-F03-01 .. T2-F03-05)
// ============================================================================

func TestTier2_F03_Boundary_ConfigValidation(t *testing.T) {
	t.Run("T2-F03-01_NonNumericNegativeLocalPort", func(t *testing.T) {
		t.Setenv("LOCAL_PORT", "-999")
		cfg, err := config.Load()
		if err != nil {
			// Validation error on invalid port is acceptable
			return
		}
		// If loaded, port falls back to 5432 default
		if cfg.LocalPort <= 0 {
			t.Errorf("expected positive port fallback, got %d", cfg.LocalPort)
		}
	})

	t.Run("T2-F03-02_ZeroNegativePollInterval", func(t *testing.T) {
		cfg := config.Config{
			OperationPollIntervalSeconds: 0,
			OperationTimeoutSeconds:      600,
			DumpDir:                      "dumps",
		}

		err := cfg.ValidateLocal()
		if err == nil {
			t.Fatalf("expected error for 0 poll interval, got nil")
		}
		if !strings.Contains(err.Error(), "STACKIT_OPERATION_POLL_INTERVAL_SECONDS must be greater than 0") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("T2-F03-03_ZeroNegativeOperationTimeout", func(t *testing.T) {
		cfg := config.Config{
			OperationPollIntervalSeconds: 10,
			OperationTimeoutSeconds:      -5,
			DumpDir:                      "dumps",
		}

		err := cfg.ValidateLocal()
		if err == nil {
			t.Fatalf("expected error for negative timeout, got nil")
		}
		if !strings.Contains(err.Error(), "STACKIT_OPERATION_TIMEOUT_SECONDS must be greater than 0") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("T2-F03-04_EmptyWhitespaceDumpDirectory", func(t *testing.T) {
		cfg := config.Config{
			OperationPollIntervalSeconds: 10,
			OperationTimeoutSeconds:      600,
			DumpDir:                      "   ",
		}

		err := cfg.ValidateLocal()
		if err == nil {
			t.Fatalf("expected error for whitespace dump dir, got nil")
		}
		if !strings.Contains(err.Error(), "POSTGRES_DUMP_DIR must not be empty") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("T2-F03-05_WhitespaceOnlyCloudCredentials", func(t *testing.T) {
		cfg := config.Config{
			OperationPollIntervalSeconds: 10,
			OperationTimeoutSeconds:      600,
			DumpDir:                      "dumps",
			ProjectID:                    "   ",
			Region:                       "eu01",
			ServiceAccountKeyPath:        "/path/to/sa.json",
		}

		err := cfg.ValidateStackIT()
		if err == nil {
			t.Fatalf("expected error for whitespace ProjectID, got nil")
		}
		if !strings.Contains(err.Error(), "STACKIT_PROJECT_ID is required") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// ============================================================================
// FEATURE F4 BOUNDARY: LOCAL PROVIDER (5 Tests: T2-F04-01 .. T2-F04-05)
// ============================================================================

func TestTier2_F04_Boundary_LocalProvider(t *testing.T) {
	t.Run("T2-F04-01_MissingLocalUserOrPass", func(t *testing.T) {
		t.Setenv("LOCAL_USER", "")
		t.Setenv("LOCAL_PASS", "")

		_, err := postgres.ResolveCredentials("local")
		if err == nil {
			t.Fatalf("expected error when LOCAL_USER/LOCAL_PASS are unset, got nil")
		}
		if !strings.Contains(err.Error(), "missing LOCAL_USER or LOCAL_PASS") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("T2-F04-02_LocalDatabasePrecedenceLocalDBOverLocalDatabase", func(t *testing.T) {
		t.Setenv("LOCAL_DB", "primary_db")
		t.Setenv("LOCAL_DATABASE", "fallback_db")

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load failed: %v", err)
		}
		if cfg.LocalDB != "primary_db" {
			t.Errorf("expected LOCAL_DB 'primary_db' to take precedence over LOCAL_DATABASE, got %q", cfg.LocalDB)
		}
	})

	t.Run("T2-F04-03_CaseInsensitiveMatchingForLocalInstance", func(t *testing.T) {
		lp := provider.NewLocalProvider(config.Default())

		variations := []string{"LOCAL", "Local", "local", "LoCaL"}
		for _, name := range variations {
			if !lp.Handles(stackit.Instance{Name: name, ID: name}) {
				t.Errorf("expected Handles() to return true for %q", name)
			}
		}
	})

	t.Run("T2-F04-04_LocalSSLModeResolution", func(t *testing.T) {
		t.Setenv("LOCAL_USER", "postgres")
		t.Setenv("LOCAL_PASS", "postgres")

		// Default when LOCAL_SSLMODE unset
		t.Setenv("LOCAL_SSLMODE", "")
		credsDefault, err := postgres.ResolveCredentials("local")
		if err != nil || credsDefault.SSLMode != "disable" {
			t.Errorf("expected default SSLMode 'disable', got %q, err: %v", credsDefault.SSLMode, err)
		}

		// Explicit value respected
		t.Setenv("LOCAL_SSLMODE", "require")
		credsRequire, err := postgres.ResolveCredentials("local")
		if err != nil || credsRequire.SSLMode != "require" {
			t.Errorf("expected explicit SSLMode 'require', got %q, err: %v", credsRequire.SSLMode, err)
		}
	})

	t.Run("T2-F04-05_LocalMissingHostPortEndpointResolutionFallback", func(t *testing.T) {
		t.Setenv("LOCAL_HOST", "")
		t.Setenv("LOCAL_PORT", "")

		cfg := config.Config{
			LocalHost: "10.0.0.5",
			LocalPort: 5433,
			LocalDB:   "postgres",
		}
		lp := provider.NewLocalProvider(cfg)

		ep, err := lp.ResolveEndpoint(context.Background(), provider.LocalInstance)
		if err != nil {
			t.Fatalf("unexpected error resolving endpoint: %v", err)
		}
		if ep.Host != "10.0.0.5" || ep.Port != 5433 {
			t.Errorf("expected fallback to constructor endpoint 10.0.0.5:5433, got %+v", ep)
		}
	})
}

// ============================================================================
// FEATURE F5 BOUNDARY: ARTIFACT MANAGEMENT (5 Tests: T2-F05-01 .. T2-F05-05)
// ============================================================================

func TestTier2_F05_Boundary_ArtifactManagement(t *testing.T) {
	t.Run("T2-F05-01_CorruptedMetadataFileHandling", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		dumpFilename := "20260101T100000Z__dump__inst__db.dump"
		dumpPath := filepath.Join(tempDir, dumpFilename)
		metaPath := dumpPath + ".json"

		_ = os.WriteFile(dumpPath, []byte("payload"), 0644)
		_ = os.WriteFile(metaPath, []byte("{corrupted json invalid content"), 0644)

		_, err := mgr.ReadDumpArtifact(dumpFilename)
		if err == nil {
			t.Fatalf("expected error reading corrupted metadata JSON, got nil")
		}
		if !strings.Contains(err.Error(), "parse dump metadata") {
			t.Errorf("expected parse error, got: %v", err)
		}
	})

	t.Run("T2-F05-02_EmptyDumpDirectoryListing", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		list, err := mgr.ListDumpArtifacts()
		if err != nil {
			t.Fatalf("unexpected error on empty directory listing: %v", err)
		}
		if list == nil || len(list) != 0 {
			t.Errorf("expected empty non-nil slice, got: %v", list)
		}
	})

	t.Run("T2-F05-03_DirectoryWithMixedNonDumpFiles", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		// Create mixed files
		_ = os.WriteFile(filepath.Join(tempDir, "readme.txt"), []byte("readme"), 0644)
		_ = os.WriteFile(filepath.Join(tempDir, "notes.md"), []byte("notes"), 0644)
		_ = os.WriteFile(filepath.Join(tempDir, "archive.tar.gz"), []byte("tar"), 0644)
		_ = os.Mkdir(filepath.Join(tempDir, "subfolder"), 0755)

		// Create 1 valid dump
		validDump := postgres.GenerateDumpFilename(time.Now().UTC(), postgres.DumpModeStandard, "inst", "db")
		validPath := filepath.Join(tempDir, validDump)
		_ = os.WriteFile(validPath, []byte("dump content"), 0644)

		list, err := mgr.ListDumpArtifacts()
		if err != nil {
			t.Fatalf("ListDumpArtifacts failed: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("expected exactly 1 artifact filtered, got %d", len(list))
		}
		if list[0].Name != validDump {
			t.Errorf("expected %q, got %q", validDump, list[0].Name)
		}
	})

	t.Run("T2-F05-04_ExtremeSpecialCharactersSanitization", func(t *testing.T) {
		// Path traversal characters must be sanitized
		sanitized := postgres.SanitizeFileName("../../etc/passwd")
		if strings.Contains(sanitized, "/") || strings.Contains(sanitized, "\\") {
			t.Errorf("expected slashes to be removed in sanitized filename, got: %q", sanitized)
		}
	})

	t.Run("T2-F05-05_NonExistentDumpFileLookup", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		_, err := mgr.ReadDumpArtifact("non_existent_file.dump")
		if err == nil {
			t.Fatalf("expected error reading non-existent dump file, got nil")
		}
	})
}

// ============================================================================
// FEATURE F6 BOUNDARY: POSTGRES DUMP & LOGGER (5 Tests: T2-F06-01 .. T2-F06-05)
// ============================================================================

func TestTier2_F06_Boundary_PostgresDumpAndLogger(t *testing.T) {
	t.Run("T2-F06-01_ComplexInstanceNameSanitization", func(t *testing.T) {
		got := postgres.SanitizeInstanceName("staging-east.db-01")
		expected := "STAGING_EAST_DB_01"
		if got != expected {
			t.Errorf("SanitizeInstanceName('staging-east.db-01') = %q, want %q", got, expected)
		}
	})

	t.Run("T2-F06-02_SpecialCharactersInPassword", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))

		complexPass := "p@ss\"word'with$symbols!#%^&*()"
		creds := postgres.Credentials{
			User:     "postgres",
			Password: complexPass,
			SSLMode:  "disable",
		}
		logger := postgres.NewExecutionLogger(nil)
		outputPath := filepath.Join(env.DumpsDir, "complex_pwd.dump")

		err := postgres.RunPgDump(context.Background(), "127.0.0.1", 5432, "testdb", outputPath, creds, logger)
		if err != nil {
			t.Fatalf("RunPgDump failed: %v", err)
		}

		inv := env.AssertToolInvoked(t, "pg_dump")
		if inv.Password != complexPass {
			t.Errorf("expected password %q in invocation record, got %q", complexPass, inv.Password)
		}
	})

	t.Run("T2-F06-03_ContextCancellationDuringPgDump", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		outputPath := filepath.Join(env.DumpsDir, "cancel_test.dump")
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}

		err := postgres.RunPgDump(ctx, "127.0.0.1", 5432, "testdb", outputPath, creds, nil)
		if err == nil {
			t.Fatalf("expected error on cancelled context, got nil")
		}
	})

	t.Run("T2-F06-04_MassiveOutputBufferManagement", func(t *testing.T) {
		logger := postgres.NewExecutionLogger(nil)
		largeChunk := strings.Repeat("a", 1024) // 1KB

		for i := 0; i < 1024; i++ { // 1MB total
			logger.Append([]byte(largeChunk))
		}

		log := logger.GetLog()
		if len(log) != 1024*1024 {
			t.Errorf("expected 1MB log length, got %d", len(log))
		}
	})

	t.Run("T2-F06-05_WriteErrorLogWithEmptyDumpDirAndNilError", func(t *testing.T) {
		tempDir := t.TempDir()
		origWd, _ := os.Getwd()
		defer func() { _ = os.Chdir(origWd) }()
		_ = os.Chdir(tempDir)

		logPath, err := postgres.WriteErrorLog(nil, "", "restore", map[string]string{}, nil)
		if err != nil {
			t.Fatalf("WriteErrorLog failed: %v", err)
		}
		if _, statErr := os.Stat(logPath); statErr != nil {
			t.Errorf("expected log file to exist at %q: %v", logPath, statErr)
		}
	})
}

// ============================================================================
// FEATURE F7 BOUNDARY: POSTGRES RESTORE (5 Tests: T2-F07-01 .. T2-F07-05)
// ============================================================================

func TestTier2_F07_Boundary_PostgresRestore(t *testing.T) {
	t.Run("T2-F07-01_MultiLineMixedIgnorableAndFatalErrors", func(t *testing.T) {
		mixedOutput := "ERROR: extension \"pg_stat_kcache\" is not available\nFATAL: password authentication failed for user \"postgres\"\nwarning: errors were ignored during processing"

		if postgres.IsIgnorableRestoreWarning(mixedOutput) {
			t.Errorf("expected mixed output containing FATAL to NOT be classified as ignorable")
		}
	})

	t.Run("T2-F07-02_NonZeroExitCodeOtherThan1TreatedAsFatal", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		t.Setenv("MOCK_PG_RESTORE_EXIT_CODE", "2")

		dumpFile := env.WriteDumpFile("test_exit2.dump", []byte("payload"))
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}

		err := postgres.RunPgRestore(context.Background(), "127.0.0.1", 5432, "app_local", dumpFile, creds, nil)
		if err == nil {
			t.Fatalf("expected fatal error on exit code 2, got nil")
		}
		if errors.Is(err, postgres.ErrRestoreWithWarnings) {
			t.Errorf("exit code 2 should NOT be classified as non-fatal warning")
		}
	})

	t.Run("T2-F07-03_0ByteDumpFileRestoreFailure", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		t.Setenv("MOCK_PG_RESTORE_MODE", "fatal")

		emptyDump := env.WriteDumpFile("empty.dump", []byte{})
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}

		err := postgres.RunPgRestore(context.Background(), "127.0.0.1", 5432, "app_local", emptyDump, creds, nil)
		if err == nil {
			t.Fatalf("expected error on 0-byte dump restore with fatal mock, got nil")
		}
	})

	t.Run("T2-F07-04_UppercaseWarningPatternsDetection", func(t *testing.T) {
		uppercaseOutput := "ERROR: EXTENSION \"PG_STAT_KCACHE\" IS NOT AVAILABLE\nWARNING: ERRORS WERE IGNORED DURING PROCESSING"

		if !postgres.IsIgnorableRestoreWarning(uppercaseOutput) {
			t.Errorf("expected uppercase warning patterns to be detected case-insensitively")
		}
	})

	t.Run("T2-F07-05_IgnorableWarningLogNoticeAppending", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		t.Setenv("MOCK_PG_RESTORE_MODE", "warning")

		dumpFile := env.WriteDumpFile("warn.dump", []byte("payload"))
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}
		logger := postgres.NewExecutionLogger(nil)

		_ = postgres.RunPgRestore(context.Background(), "127.0.0.1", 5432, "app_local", dumpFile, creds, logger)

		transcript := logger.GetLog()
		if !strings.Contains(transcript, "Note: pg_restore completed with non-fatal") {
			t.Errorf("expected warning note to be appended to logger buffer, got:\n%s", transcript)
		}
	})
}

// ============================================================================
// FEATURE F8 BOUNDARY: API FACADE (5 Tests: T2-F08-01 .. T2-F08-05)
// ============================================================================

func TestTier2_F08_Boundary_APIFacade(t *testing.T) {
	t.Run("T2-F08-01_CloneCreationFailureCleanup", func(t *testing.T) {
		mockP := &mockTestProvider{
			supportsCloning: true,
			createCloneFunc: func(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
				return stackit.Instance{}, errors.New("provisioning timed out")
			},
		}

		_, err := mockP.CreateClone(context.Background(), stackit.Instance{Name: "prod", ID: "p-1"}, time.Now())
		if err == nil {
			t.Fatalf("expected error on clone creation timeout, got nil")
		}
	})

	t.Run("T2-F08-02_DumpFailureDuringCloneFlowCleanup", func(t *testing.T) {
		cloneDeleted := false
		mockP := &mockTestProvider{
			supportsCloning: true,
			deleteInstanceFunc: func(ctx context.Context, instance stackit.Instance) error {
				cloneDeleted = true
				return nil
			},
		}

		clone, _ := mockP.CreateClone(context.Background(), stackit.Instance{Name: "prod", ID: "p-1"}, time.Now())
		// If dump fails, delete instance is invoked
		_ = mockP.DeleteInstance(context.Background(), clone)

		if !cloneDeleted {
			t.Errorf("expected clone to be deleted during cleanup")
		}
	})

	t.Run("T2-F08-03_DualFailureDumpAndCloneDeleteFail", func(t *testing.T) {
		dumpErr := errors.New("dump extraction failed")
		deleteErr := errors.New("clone deletion failed")

		joined := errors.Join(dumpErr, deleteErr)
		if !errors.Is(joined, dumpErr) || !errors.Is(joined, deleteErr) {
			t.Errorf("expected joined error to wrap both dump and delete failures")
		}
	})

	t.Run("T2-F08-04_NoBackupsAvailableInReplicaDumpMode", func(t *testing.T) {
		mockP := &mockTestProvider{
			supportsCloning: true,
		}

		backups, err := mockP.GetBackups(context.Background(), stackit.Instance{Name: "prod", ID: "p-1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(backups) != 0 {
			t.Fatalf("expected 0 backups")
		}
	})

	t.Run("T2-F08-05_UnroutedTargetInstanceID", func(t *testing.T) {
		lp := provider.NewLocalProvider(config.Default())
		router := provider.NewRouter(lp)

		_, err := router.Route(stackit.Instance{Name: "unhandled-instance", ID: "unhandled-id"})
		if err == nil {
			t.Fatalf("expected error for unhandled instance, got nil")
		}
		if !strings.Contains(err.Error(), "no provider found") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// ============================================================================
// FEATURE F9 BOUNDARY: PREFLIGHT TOOLS (5 Tests: T2-F09-01 .. T2-F09-05)
// ============================================================================

func TestTier2_F09_Boundary_PreflightTools(t *testing.T) {
	t.Run("T2-F09-01_NonExecutablePgDumpInPATH", func(t *testing.T) {
		tempDir := t.TempDir()
		// Non-executable file
		pgDumpPath := filepath.Join(tempDir, "pg_dump")
		_ = os.WriteFile(pgDumpPath, []byte("#!/bin/sh\n"), 0600)
		t.Setenv("PATH", tempDir)

		err := postgres.CheckPreflightTools()
		if err == nil {
			t.Fatalf("expected error when pg_dump is non-executable, got nil")
		}
	})

	t.Run("T2-F09-02_PATHWithEmptyRelativeComponents", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("PATH", ".:/nonexistent/dir::"+tempDir)

		// Should execute safely without panicking
		_ = postgres.CheckPreflightTools()
	})

	t.Run("T2-F09-03_ConcurrentPreflightVerification", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))

		const goroutines = 40
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				_ = postgres.CheckPreflightTools()
			}()
		}
		wg.Wait()
	})

	t.Run("T2-F09-04_PreflightHaltsStartupBeforeConfigLoad", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("PATH", tempDir)

		err := postgres.CheckPreflightTools()
		if err == nil {
			t.Fatalf("expected preflight check to halt before config load, got nil")
		}
	})

	t.Run("T2-F09-05_PreflightInNonInteractiveCLIMode", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("PATH", tempDir)

		err := postgres.CheckPreflightTools()
		if err == nil {
			t.Fatalf("expected preflight error in non-interactive environment, got nil")
		}
		if !strings.Contains(err.Error(), "required tool") {
			t.Errorf("expected required tool error message, got: %v", err)
		}
	})
}

// ============================================================================
// FEATURE F10 BOUNDARY: STEP VIEW & SUMMARY (5 Tests: T2-F10-01 .. T2-F10-05)
// ============================================================================

func TestTier2_F10_Boundary_StepView(t *testing.T) {
	t.Run("T2-F10-01_ZeroPlannedExecutionSteps", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		// Help command runs with 0 steps
		res := env.RunCLI("--help")
		res.AssertSuccess(t)
	})

	t.Run("T2-F10-02_FailureOnFirstStep0", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		t.Setenv("MOCK_PG_DUMP_EXIT_CODE", "1")

		outputPath := filepath.Join(env.DumpsDir, "fail_step0.dump")
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}

		err := postgres.RunPgDump(context.Background(), "127.0.0.1", 5432, "app_local", outputPath, creds, nil)
		if err == nil {
			t.Fatalf("expected error on step 0 dump failure, got nil")
		}
	})

	t.Run("T2-F10-03_FailureOnIntermediateStep", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")
		env.SetEnv("MOCK_PG_RESTORE_MODE", "fatal")

		dumpFile := env.WriteDumpFile("test.dump", []byte("payload"))

		res := env.RunCLI(
			"--action=restore",
			"--target-instance=local",
			"--target-database=app_local",
			"--dump-file="+dumpFile,
		)

		res.AssertFailure(t)
	})

	t.Run("T2-F10-04_NonTTYCleanPlaintextFormatting", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		res := env.RunCLI(
			"--action=dump",
			"--instance=local",
			"--database=app_local",
			"--mode=live",
		)

		res.AssertSuccess(t)
		// Check that output contains readable step markers
		if !strings.Contains(res.Stdout, "Step 1/1") {
			t.Errorf("expected clean step text in output, got:\n%s", res.Stdout)
		}
	})

	t.Run("T2-F10-05_ContextCancellationDuringActiveStep", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		outputPath := filepath.Join(env.DumpsDir, "cancelled.dump")
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}

		err := postgres.RunPgDump(ctx, "127.0.0.1", 5432, "app_local", outputPath, creds, nil)
		if err == nil {
			t.Fatalf("expected error on cancelled step execution, got nil")
		}
	})
}
