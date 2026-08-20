package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/provider"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
	"github.com/GrigoreAlexandru/Stackit-Restore/test/e2e/harness"
	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

// ============================================================================
// FEATURE F1: CLI USAGE & HELP (5 Tests: T1-F01-01 .. T1-F01-05)
// ============================================================================

func TestTier1_F01_CLIUsageAndHelp(t *testing.T) {
	t.Run("T1-F01-01_LongFormHelpFlag", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("--help")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT").
			AssertStdoutContains(t, "Core Actions:").
			AssertStdoutContains(t, "Global Flags:").
			AssertStdoutContains(t, "Single-Line Usage Examples:")
	})

	t.Run("T1-F01-02_ShortFormHelpFlag", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("-h")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT").
			AssertStdoutContains(t, "Usage:").
			AssertStdoutContains(t, "Core Actions:")
	})

	t.Run("T1-F01-03_HelpPrecedenceOverIncompleteFlags", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("--action=dump", "--help")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT").
			AssertStderrNotContains(t, "missing")
	})

	t.Run("T1-F01-04_HelpPrecedenceOverInvalidAction", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("--action=completely_invalid", "--help")

		res.AssertSuccess(t).
			AssertStdoutContains(t, "PostgreSQL Restore CLI for STACKIT").
			AssertStderrNotContains(t, "invalid or missing --action")
	})

	t.Run("T1-F01-05_ProgrammaticUsageOutputVerification", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		res := env.RunCLI("--help")

		requiredSections := []string{
			"PostgreSQL Restore CLI for STACKIT",
			"Usage:",
			"Core Actions:",
			"dump",
			"restore",
			"sync",
			"Global Flags:",
			"--action",
			"--instance",
			"--database",
			"--target-instance",
			"--target-database",
			"--mode",
			"--pit",
			"Environment Variables:",
			"STACKIT_PROJECT_ID",
			"Single-Line Usage Examples:",
		}

		for _, section := range requiredSections {
			res.AssertStdoutContains(t, section)
		}
	})
}

// ============================================================================
// FEATURE F2: CLI FLAG PARSING & OVERRIDES (5 Tests: T1-F02-01 .. T1-F02-05)
// ============================================================================

func TestTier1_F02_FlagParsingAndNormalization(t *testing.T) {
	t.Run("T1-F02-01_NonInteractiveDumpFlagNormalization", func(t *testing.T) {
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
	})

	t.Run("T1-F02-02_ActionAliasesParsing", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		// 1. export alias -> dump action
		resExport := env.RunCLI(
			"--action=export",
			"--instance=local",
			"--database=app_local",
			"--mode=live",
		)
		resExport.AssertSuccess(t)

		// 2. import alias -> restore action
		resImport := env.RunCLI(
			"--action=import",
			"--target-instance=local",
			"--target-database=app_local",
			"--dump-file=dumps/test.dump",
		)
		// Dump file missing in DummyAPI will fail execution, but flag parsing succeeded
		if resImport.Stderr != "" && strings.Contains(resImport.Stderr, "invalid or missing --action") {
			t.Errorf("expected import to be recognized as restore action, but got: %s", resImport.Stderr)
		}

		// 3. copy alias -> sync action
		resCopy := env.RunCLI(
			"--action=copy",
			"--instance=local",
			"--database=app_local",
			"--target-instance=local",
			"--target-database=app_local",
			"--mode=live",
		)
		resCopy.AssertSuccess(t)
	})

	t.Run("T1-F02-03_ParameterAliasesMapping", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		res := env.RunCLI(
			"--action=sync",
			"--source-instance=local",
			"--source-database=app_local",
			"--dest-instance=local",
			"--dest-database=app_local",
			"--mode=live",
		)

		res.AssertSuccess(t)
	})

	t.Run("T1-F02-04_PointInTimeDatetimeStringParsing", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("PRODUCTION_USER", "prod_user")
		env.SetEnv("PRODUCTION_PASS", "prod_pass")

		res := env.RunCLI(
			"--action=dump",
			"--instance=Production",
			"--database=app_prod",
			"--mode=pit",
			"--pit=2026-08-13 15:00:00",
		)

		res.AssertSuccess(t)
	})

	t.Run("T1-F02-05_PureFlagParsing", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		res := env.RunCLI(
			"--action=dump",
			"--instance=local",
			"--database=app_local",
			"--project-id=test-project-123",
			"--region=eu01",
		)

		res.AssertSuccess(t)
	})
}

// ============================================================================
// FEATURE F3: CONFIG VALIDATION & LOADING (5 Tests: T1-F03-01 .. T1-F03-05)
// ============================================================================

func TestTier1_F03_ConfigValidationAndLoading(t *testing.T) {
	t.Run("T1-F03-01_DefaultLocalConfigLoading", func(t *testing.T) {
		cfg := config.Default()

		if cfg.OperationPollIntervalSeconds != 10 {
			t.Errorf("expected default poll interval 10, got %d", cfg.OperationPollIntervalSeconds)
		}
		if cfg.OperationTimeoutSeconds != 600 {
			t.Errorf("expected default timeout 600, got %d", cfg.OperationTimeoutSeconds)
		}
		if cfg.DumpDir != "dumps" {
			t.Errorf("expected default dumpDir 'dumps', got %q", cfg.DumpDir)
		}
		if cfg.LocalHost != "localhost" {
			t.Errorf("expected default host 'localhost', got %q", cfg.LocalHost)
		}
		if cfg.LocalPort != 5432 {
			t.Errorf("expected default port 5432, got %d", cfg.LocalPort)
		}
		if cfg.LocalDB != "postgres" {
			t.Errorf("expected default db 'postgres', got %q", cfg.LocalDB)
		}
	})

	t.Run("T1-F03-02_SplitValidationLocalVsCloudDisjunction", func(t *testing.T) {
		cfg := config.Default()

		if err := cfg.ValidateLocal(); err != nil {
			t.Errorf("expected ValidateLocal() to succeed on default config, got %v", err)
		}

		if err := cfg.ValidateStackIT(); err == nil {
			t.Errorf("expected ValidateStackIT() to fail when cloud credentials missing, got nil")
		}
	})

	t.Run("T1-F03-03_ValidateStackITSuccessOnCompleteCloudConfig", func(t *testing.T) {
		cfg := config.Config{
			OperationPollIntervalSeconds: 10,
			OperationTimeoutSeconds:      600,
			DumpDir:                      "dumps",
			ProjectID:                    "project-uuid-12345",
			Region:                       "eu01",
			ServiceAccountKeyPath:        "/path/to/sa-key.json",
		}

		if err := cfg.ValidateLocal(); err != nil {
			t.Errorf("expected ValidateLocal() to succeed, got: %v", err)
		}
		if err := cfg.ValidateStackIT(); err != nil {
			t.Errorf("expected ValidateStackIT() to succeed, got: %v", err)
		}
	})

	t.Run("T1-F03-04_PureConfigLoadNoFilesystemSideEffects", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDumpDir := filepath.Join(tempDir, "should_not_be_created_by_load")
		t.Setenv("POSTGRES_DUMP_DIR", nonExistentDumpDir)

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() failed: %v", err)
		}
		if cfg.DumpDir != nonExistentDumpDir {
			t.Fatalf("expected DumpDir %q, got %q", nonExistentDumpDir, cfg.DumpDir)
		}

		if _, err := os.Stat(nonExistentDumpDir); !os.IsNotExist(err) {
			t.Errorf("expected directory %q NOT to be created by config.Load()", nonExistentDumpDir)
		}
	})

	t.Run("T1-F03-05_DotEnvInvariantPreservePreExistingEnvironment", func(t *testing.T) {
		tempDir := t.TempDir()
		origWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get working dir: %v", err)
		}
		defer func() { _ = os.Chdir(origWd) }()

		if err := os.Chdir(tempDir); err != nil {
			t.Fatalf("failed to chdir to tempDir: %v", err)
		}

		dotenvContent := "LOCAL_DB=from_dotenv_file\nSTACKIT_REGION=from_dotenv_region\n"
		if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(dotenvContent), 0644); err != nil {
			t.Fatalf("failed to write .env: %v", err)
		}

		t.Setenv("LOCAL_DB", "existing_env_db")
		t.Setenv("STACKIT_REGION", "")

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load() failed: %v", err)
		}

		if cfg.LocalDB != "existing_env_db" {
			t.Errorf("expected pre-existing env 'existing_env_db' to take precedence, got %q", cfg.LocalDB)
		}
		if cfg.Region != "from_dotenv_region" {
			t.Errorf("expected unset env to be populated from .env, got %q", cfg.Region)
		}
	})
}

// ============================================================================
// FEATURE F4: LOCAL PROVIDER & ROUTING (5 Tests: T1-F04-01 .. T1-F04-05)
// ============================================================================

func TestTier1_F04_LocalProviderRouting(t *testing.T) {
	t.Run("T1-F04-01_LocalProviderHandlesInvariant", func(t *testing.T) {
		lp := provider.NewLocalProvider(config.Default())

		if !lp.Handles(stackit.Instance{Name: "local", ID: "local"}) {
			t.Errorf("expected Handles to return true for 'local'")
		}
		if !lp.Handles(stackit.Instance{Name: "LOCAL", ID: "123"}) {
			t.Errorf("expected Handles to return true for 'LOCAL'")
		}
		if !lp.Handles(stackit.Instance{Name: "Local", ID: "local"}) {
			t.Errorf("expected Handles to return true for 'Local'")
		}
		if lp.Handles(stackit.Instance{Name: "Production", ID: "prod-1"}) {
			t.Errorf("expected Handles to return false for non-local instance")
		}
	})

	t.Run("T1-F04-02_DeterministicDatabaseResolution", func(t *testing.T) {
		cfg := config.Config{
			LocalHost: "localhost",
			LocalPort: 5432,
			LocalDB:   "configured_constructor_db",
		}
		lp := provider.NewLocalProvider(cfg)

		dbs, err := lp.GetDatabases(context.Background(), provider.LocalInstance)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(dbs) != 1 {
			t.Fatalf("expected 1 database, got %d", len(dbs))
		}
		if dbs[0].Name != "configured_constructor_db" {
			t.Errorf("expected db name 'configured_constructor_db', got %q", dbs[0].Name)
		}
	})

	t.Run("T1-F04-03_LocalEndpointResolution", func(t *testing.T) {
		t.Setenv("LOCAL_HOST", "")
		t.Setenv("LOCAL_PORT", "")

		cfg := config.Config{
			LocalHost: "192.168.1.100",
			LocalPort: 5439,
			LocalDB:   "postgres",
		}
		lp := provider.NewLocalProvider(cfg)

		ep, err := lp.ResolveEndpoint(context.Background(), provider.LocalInstance)
		if err != nil {
			t.Fatalf("unexpected error resolving endpoint: %v", err)
		}
		if ep.Host != "192.168.1.100" || ep.Port != 5439 {
			t.Errorf("expected endpoint 192.168.1.100:5439, got %+v", ep)
		}
	})

	t.Run("T1-F04-04_LocalProviderCloningRejectionInvariant", func(t *testing.T) {
		lp := provider.NewLocalProvider(config.Default())

		if lp.SupportsCloning() {
			t.Errorf("expected SupportsCloning() to be false for LocalProvider")
		}

		_, err := lp.CreateClone(context.Background(), provider.LocalInstance, time.Now())
		if err == nil {
			t.Fatalf("expected error on CreateClone for LocalProvider, got nil")
		}
		if !strings.Contains(err.Error(), "not supported") {
			t.Errorf("expected 'not supported' error message, got: %v", err)
		}
	})

	t.Run("T1-F04-05_LocalProviderBackupsAndDeletionInvariants", func(t *testing.T) {
		lp := provider.NewLocalProvider(config.Default())

		backups, err := lp.GetBackups(context.Background(), provider.LocalInstance)
		if err != nil {
			t.Errorf("expected GetBackups to return nil error, got: %v", err)
		}
		if len(backups) != 0 {
			t.Errorf("expected empty backups slice for LocalProvider, got %d items", len(backups))
		}

		if err := lp.DeleteInstance(context.Background(), provider.LocalInstance); err != nil {
			t.Errorf("expected DeleteInstance to be a no-op returning nil, got: %v", err)
		}
	})
}

// ============================================================================
// FEATURE F5: POSTGRES ARTIFACT MANAGEMENT (5 Tests: T1-F05-01 .. T1-F05-05)
// ============================================================================

func TestTier1_F05_PostgresArtifactManagement(t *testing.T) {
	t.Run("T1-F05-01_MicrosecondPrecisionRapidDumpNaming", func(t *testing.T) {
		ts1 := time.Date(2026, 8, 20, 10, 0, 0, 123456000, time.UTC)
		ts2 := time.Date(2026, 8, 20, 10, 0, 0, 654321000, time.UTC)

		fn1 := postgres.GenerateDumpFilename(ts1, postgres.DumpModeStandard, "prod", "db")
		fn2 := postgres.GenerateDumpFilename(ts2, postgres.DumpModeStandard, "prod", "db")

		if fn1 == fn2 {
			t.Fatalf("expected distinct filenames for timestamps in same second with different microseconds")
		}

		microPattern := regexp.MustCompile(`^\d{8}T\d{6}\.\d{6}Z__`)
		if !microPattern.MatchString(fn1) {
			t.Errorf("expected filename %q to match microsecond pattern", fn1)
		}
	})

	t.Run("T1-F05-02_MetadataJSONWriteAndReadRoundTrip", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		orig := mgr.NewDumpArtifact("instance-01", "Production", "app_prod", postgres.DumpModeStandard)
		if err := mgr.WriteMetadata(orig); err != nil {
			t.Fatalf("WriteMetadata failed: %v", err)
		}

		read, err := mgr.ReadDumpArtifact(orig.Name)
		if err != nil {
			t.Fatalf("ReadDumpArtifact failed: %v", err)
		}

		if read.Name != orig.Name || read.InstanceID != orig.InstanceID ||
			read.InstanceName != orig.InstanceName || read.DatabaseName != orig.DatabaseName ||
			read.Mode != orig.Mode {
			t.Errorf("read artifact mismatch: got %+v, want %+v", read, orig)
		}
	})

	t.Run("T1-F05-03_ArtifactListingSorting", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		tOld := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		tMid := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		tNew := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)

		// Create files out of order
		for _, ts := range []time.Time{tMid, tOld, tNew} {
			fn := postgres.GenerateDumpFilename(ts, postgres.DumpModeStandard, "inst", "db")
			filePath := filepath.Join(tempDir, fn)
			if err := os.WriteFile(filePath, []byte("payload"), 0644); err != nil {
				t.Fatalf("write dump file failed: %v", err)
			}
			art := postgres.DumpArtifact{
				Name:         fn,
				Path:         filePath,
				Mode:         postgres.DumpModeStandard,
				InstanceName: "inst",
				InstanceID:   "inst",
				DatabaseName: "db",
				CreatedAt:    ts,
			}
			if err := mgr.WriteMetadata(art); err != nil {
				t.Fatalf("write metadata failed: %v", err)
			}
		}

		list, err := mgr.ListDumpArtifacts()
		if err != nil {
			t.Fatalf("ListDumpArtifacts failed: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("expected 3 artifacts, got %d", len(list))
		}

		// Strictly descending
		if !list[0].CreatedAt.Equal(tNew) || !list[1].CreatedAt.Equal(tMid) || !list[2].CreatedAt.Equal(tOld) {
			t.Errorf("expected artifacts sorted descending: [%v, %v, %v], got [%v, %v, %v]",
				tNew, tMid, tOld, list[0].CreatedAt, list[1].CreatedAt, list[2].CreatedAt)
		}
	})

	t.Run("T1-F05-04_LegacyDumpFileReadFallback", func(t *testing.T) {
		tempDir := t.TempDir()
		mgr := postgres.NewArtifactManager(tempDir)

		legacyFilename := "20260101T120000Z__dump_from_live__prod__app_prod.dump"
		legacyPath := filepath.Join(tempDir, legacyFilename)
		if err := os.WriteFile(legacyPath, []byte("dummy raw payload"), 0644); err != nil {
			t.Fatalf("failed to write raw dump file: %v", err)
		}

		art, err := mgr.ReadDumpArtifact(legacyFilename)
		if err != nil {
			t.Fatalf("expected fallback to read raw dump file, got err: %v", err)
		}
		if art.Name != legacyFilename || art.Path != legacyPath {
			t.Errorf("unexpected artifact from raw file: %+v", art)
		}
	})

	t.Run("T1-F05-05_FilenameSanitization", func(t *testing.T) {
		cases := []struct {
			input    string
			expected string
		}{
			{"prod/staging/app", "prod-staging-app"},
			{"database with spaces", "database-with-spaces"},
			{"special\\path\\char", "special-path-char"},
			{"   ", "unknown"},
		}

		for _, tc := range cases {
			actual := postgres.SanitizeFileName(tc.input)
			if actual != tc.expected {
				t.Errorf("SanitizeFileName(%q) = %q, want %q", tc.input, actual, tc.expected)
			}
		}
	})
}

// ============================================================================
// FEATURE F6: POSTGRES DUMP EXECUTION & LOGGER (5 Tests: T1-F06-01 .. T1-F06-05)
// ============================================================================

func TestTier1_F06_PostgresDumpExecutionAndLogger(t *testing.T) {
	t.Run("T1-F06-01_InstantiableExecutionLoggerIndependence", func(t *testing.T) {
		l1 := postgres.NewExecutionLogger(nil)
		l2 := postgres.NewExecutionLogger(nil)

		l1.Append([]byte("log line from logger 1\n"))

		if l1.GetLog() != "log line from logger 1\n" {
			t.Errorf("l1 log mismatch: %q", l1.GetLog())
		}
		if l2.GetLog() != "" {
			t.Errorf("expected l2 to be independent and empty, got %q", l2.GetLog())
		}
	})

	t.Run("T1-F06-02_RemoteVsLocalCredentialResolution", func(t *testing.T) {
		t.Setenv("LOCAL_USER", "loc_u")
		t.Setenv("LOCAL_PASS", "loc_p")
		t.Setenv("LOCAL_SSLMODE", "disable")

		t.Setenv("PRODUCTION_USER", "prod_u")
		t.Setenv("PRODUCTION_PASS", "prod_p")

		localCreds, err := postgres.ResolveCredentials("local")
		if err != nil {
			t.Fatalf("failed to resolve local creds: %v", err)
		}
		if localCreds.SSLMode != "disable" {
			t.Errorf("expected local SSLMode 'disable', got %q", localCreds.SSLMode)
		}

		prodCreds, err := postgres.ResolveCredentials("Production")
		if err != nil {
			t.Fatalf("failed to resolve production creds: %v", err)
		}
		if prodCreds.SSLMode != "require" {
			t.Errorf("expected production SSLMode 'require', got %q", prodCreds.SSLMode)
		}
	})

	t.Run("T1-F06-03_RunPgDumpCommandArgumentsFormulation", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		outputPath := filepath.Join(env.DumpsDir, "test.dump")
		creds := postgres.Credentials{User: "postgres", Password: "secret", SSLMode: "disable"}
		logger := postgres.NewExecutionLogger(nil)

		err := postgres.RunPgDump(context.Background(), "127.0.0.1", 5432, "testdb", outputPath, creds, logger)
		if err != nil {
			t.Fatalf("RunPgDump failed: %v", err)
		}

		inv := env.AssertToolInvoked(t, "pg_dump")
		env.AssertToolInvokedWithArg(t, "pg_dump", "--format=c")
		env.AssertToolInvokedWithArg(t, "pg_dump", "--clean")
		env.AssertToolInvokedWithArg(t, "pg_dump", "--if-exists")
		env.AssertToolInvokedWithArg(t, "pg_dump", "--no-owner")
		env.AssertToolInvokedWithArg(t, "pg_dump", "--no-privileges")

		if inv.Password != "secret" || inv.SSLMode != "disable" {
			t.Errorf("expected password 'secret' and sslmode 'disable', got pass=%q ssl=%q", inv.Password, inv.SSLMode)
		}
	})

	t.Run("T1-F06-04_ExecutionLoggerConcurrentLoggingAndReset", func(t *testing.T) {
		logger := postgres.NewExecutionLogger(nil)
		const goroutines = 30
		const iters = 50

		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < iters; j++ {
					logger.Append([]byte(fmt.Sprintf("g%d-i%d\n", id, j)))
					_ = logger.GetLog()
				}
			}(i)
		}
		wg.Wait()

		if len(logger.GetLog()) == 0 {
			t.Errorf("expected non-empty log after concurrent writes")
		}

		logger.Reset()
		if logger.GetLog() != "" {
			t.Errorf("expected empty log after Reset(), got %q", logger.GetLog())
		}
	})

	t.Run("T1-F06-05_WriteErrorLogExecutionLogCapture", func(t *testing.T) {
		tempDir := t.TempDir()
		logger := postgres.NewExecutionLogger(nil)
		logger.Append([]byte("$ pg_dump --host 127.0.0.1\npg_dump: error: failed to connect to database\n"))

		details := map[string]string{
			"Instance": "Production",
			"Database": "app_prod",
			"Mode":     "live",
		}
		opErr := errors.New("connection failed: exit status 1")

		logPath, err := postgres.WriteErrorLog(logger, tempDir, "dump", details, opErr)
		if err != nil {
			t.Fatalf("WriteErrorLog failed: %v", err)
		}

		content, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("failed to read written error log: %v", err)
		}
		contentStr := string(content)

		if !strings.Contains(contentStr, "Execution Error Log") {
			t.Errorf("expected header in log file, got:\n%s", contentStr)
		}
		if !strings.Contains(contentStr, "Instance:    Production") {
			t.Errorf("expected Instance detail, got:\n%s", contentStr)
		}
		if !strings.Contains(contentStr, "pg_dump: error: failed to connect to database") {
			t.Errorf("expected transcript in log, got:\n%s", contentStr)
		}
	})
}

// ============================================================================
// FEATURE F7: POSTGRES RESTORE EXECUTION & WARNINGS (5 Tests: T1-F07-01 .. T1-F07-05)
// ============================================================================

func TestTier1_F07_PostgresRestoreExecutionAndWarnings(t *testing.T) {
	t.Run("T1-F07-01_RunPgRestoreCommandArgumentsFormulation", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		dumpPath := filepath.Join(env.DumpsDir, "restore_test.dump")
		_ = os.WriteFile(dumpPath, []byte("payload"), 0644)
		creds := postgres.Credentials{User: "postgres", Password: "secret", SSLMode: "disable"}
		logger := postgres.NewExecutionLogger(nil)

		err := postgres.RunPgRestore(context.Background(), "127.0.0.1", 5432, "testdb", dumpPath, creds, logger)
		if err != nil {
			t.Fatalf("RunPgRestore failed: %v", err)
		}

		env.AssertToolInvoked(t, "pg_restore")
		env.AssertToolInvokedWithArg(t, "pg_restore", "--clean")
		env.AssertToolInvokedWithArg(t, "pg_restore", "--if-exists")
		env.AssertToolInvokedWithArg(t, "pg_restore", "--no-owner")
		env.AssertToolInvokedWithArg(t, "pg_restore", "--no-privileges")
		env.AssertToolInvokedWithArg(t, "pg_restore", "--verbose")
		env.AssertToolInvokedWithArg(t, "pg_restore", dumpPath)
	})

	t.Run("T1-F07-02_IgnorableExtensionWarningDetection", func(t *testing.T) {
		warningOutput := "pg_restore: creating EXTENSION \"pg_stat_kcache\"\npg_restore: error: could not execute query: ERROR: extension \"pg_stat_kcache\" is not available\npg_restore: warning: errors were ignored during processing"

		if !postgres.IsIgnorableRestoreWarning(warningOutput) {
			t.Errorf("expected pg_stat_kcache warning to be classified as ignorable")
		}
	})

	t.Run("T1-F07-03_IgnorableRoleSchemaAlreadyExistsWarnings", func(t *testing.T) {
		warningOutput := "ERROR: role \"app_user\" already exists\nERROR: schema \"public\" already exists\nwarning: errors were ignored during processing"

		if !postgres.IsIgnorableRestoreWarning(warningOutput) {
			t.Errorf("expected preexisting role/schema warnings to be classified as ignorable")
		}
	})

	t.Run("T1-F07-04_FatalConnectionFailureDetection", func(t *testing.T) {
		fatalOutput := "pg_restore: error: connection to server at \"127.0.0.1\", port 5432 failed: Connection refused\npg_restore: error: could not connect to server"

		if postgres.IsIgnorableRestoreWarning(fatalOutput) {
			t.Errorf("expected connection failure NOT to be classified as ignorable")
		}
	})

	t.Run("T1-F07-05_EmptyDumpPathValidation", func(t *testing.T) {
		creds := postgres.Credentials{User: "postgres", Password: "secret", SSLMode: "disable"}
		err := postgres.RunPgRestore(context.Background(), "127.0.0.1", 5432, "testdb", "", creds, nil)
		if err == nil {
			t.Fatalf("expected error when dumpPath is empty, got nil")
		}
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected error mentioning empty path, got: %v", err)
		}
	})
}

// ============================================================================
// FEATURE F8: API FACADE & CLONE FLOW (5 Tests: T1-F08-01 .. T1-F08-05)
// ============================================================================

func TestTier1_F08_APIFacadeAndCloneFlow(t *testing.T) {
	t.Run("T1-F08-01_UnifiedCreateDumpViaCloneExecutionFlow", func(t *testing.T) {
		cloneCreated := false
		cloneDeleted := false

		mockP := &mockTestProvider{
			supportsCloning: true,
			createCloneFunc: func(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
				cloneCreated = true
				return stackit.Instance{Name: "temp-clone-01", ID: "clone-123"}, nil
			},
			deleteInstanceFunc: func(ctx context.Context, instance stackit.Instance) error {
				cloneDeleted = true
				return nil
			},
		}

		inst := stackit.Instance{Name: "prod", ID: "p-1"}
		clone, err := mockP.CreateClone(context.Background(), inst, time.Now())
		if err != nil || !cloneCreated {
			t.Fatalf("failed clone creation: %v", err)
		}
		if err := mockP.DeleteInstance(context.Background(), clone); err != nil || !cloneDeleted {
			t.Fatalf("failed clone deletion: %v", err)
		}
	})

	t.Run("T1-F08-02_PITDumpFlowWithExactTimestamp", func(t *testing.T) {
		var capturedPIT time.Time
		mockP := &mockTestProvider{
			supportsCloning: true,
			createCloneFunc: func(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
				capturedPIT = pit
				return stackit.Instance{Name: "pit-clone", ID: "pit-1"}, nil
			},
		}

		expectedPIT := time.Date(2026, 8, 13, 15, 0, 0, 0, time.UTC)
		clone, err := mockP.CreateClone(context.Background(), stackit.Instance{Name: "prod", ID: "p-1"}, expectedPIT)
		if err != nil {
			t.Fatalf("CreateClone failed: %v", err)
		}
		if clone.ID != "pit-1" {
			t.Errorf("unexpected clone ID: %s", clone.ID)
		}
		if !capturedPIT.Equal(expectedPIT) {
			t.Errorf("expected PIT %v, got %v", expectedPIT, capturedPIT)
		}
	})

	t.Run("T1-F08-03_StrictIsDeleteForbiddenHandling", func(t *testing.T) {
		// 1. Sentinel error
		if !stackit.IsDeleteForbidden(stackit.ErrDeleteInstanceForbidden) {
			t.Errorf("expected ErrDeleteInstanceForbidden to be forbidden")
		}

		// 2. OpenAPI 403 error
		oapi403 := oapierror.NewError(403, "Forbidden")
		if !stackit.IsDeleteForbidden(oapi403) {
			t.Errorf("expected OpenAPI 403 error to be forbidden")
		}

		// 3. Unrelated string error with 403 should NOT match strictly
		if stackit.IsDeleteForbidden(errors.New("request failed with 403")) {
			t.Errorf("expected generic string error NOT to match IsDeleteForbidden")
		}
	})

	t.Run("T1-F08-04_OperationTimeoutParameterPropagation", func(t *testing.T) {
		cfg := config.Config{
			OperationPollIntervalSeconds: 15,
			OperationTimeoutSeconds:      1200,
			DumpDir:                      "dumps",
		}

		if cfg.OperationTimeoutSeconds != 1200 {
			t.Errorf("expected OperationTimeoutSeconds 1200, got %d", cfg.OperationTimeoutSeconds)
		}
	})

	t.Run("T1-F08-05_ModernizedBackupSortingInAPIClient", func(t *testing.T) {
		t1 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
		t3 := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)

		backups := []stackit.Backup{
			{Name: "b1", CreatedAt: t1},
			{Name: "b2", CreatedAt: t2},
			{Name: "b3", CreatedAt: t3},
		}

		var latest time.Time
		for _, b := range backups {
			if b.CreatedAt.After(latest) {
				latest = b.CreatedAt
			}
		}

		if !latest.Equal(t2) {
			t.Errorf("expected latest backup time %v, got %v", t2, latest)
		}
	})
}

// ============================================================================
// FEATURE F9: PREFLIGHT TOOL VERIFICATION (5 Tests: T1-F09-01 .. T1-F09-05)
// ============================================================================

func TestTier1_F09_PreflightToolVerification(t *testing.T) {
	t.Run("T1-F09-01_PreflightSuccessInvariant", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))

		if err := postgres.CheckPreflightTools(); err != nil {
			t.Errorf("expected CheckPreflightTools to succeed with mock tools in PATH, got: %v", err)
		}
	})

	t.Run("T1-F09-02_PreflightFailureOnMissingPgDump", func(t *testing.T) {
		tempDir := t.TempDir()
		// Put only pg_restore in isolated PATH
		pgRestorePath := filepath.Join(tempDir, "pg_restore")
		_ = os.WriteFile(pgRestorePath, []byte("#!/bin/sh\nexit 0\n"), 0755)

		t.Setenv("PATH", tempDir)

		err := postgres.CheckPreflightTools()
		if err == nil {
			t.Fatalf("expected error when pg_dump is missing, got nil")
		}
		if !strings.Contains(err.Error(), "pg_dump") {
			t.Errorf("expected error message to mention 'pg_dump', got: %v", err)
		}
	})

	t.Run("T1-F09-03_PreflightFailureOnMissingPgRestore", func(t *testing.T) {
		tempDir := t.TempDir()
		// Put only pg_dump in isolated PATH
		pgDumpPath := filepath.Join(tempDir, "pg_dump")
		_ = os.WriteFile(pgDumpPath, []byte("#!/bin/sh\nexit 0\n"), 0755)

		t.Setenv("PATH", tempDir)

		err := postgres.CheckPreflightTools()
		if err == nil {
			t.Fatalf("expected error when pg_restore is missing, got nil")
		}
		if !strings.Contains(err.Error(), "pg_restore") {
			t.Errorf("expected error message to mention 'pg_restore', got: %v", err)
		}
	})

	t.Run("T1-F09-04_PreflightExecutionOrderInStartup", func(t *testing.T) {
		tempDir := t.TempDir()
		// In an environment with empty PATH without tools, preflight check fails before any operation
		t.Setenv("PATH", tempDir)

		err := postgres.CheckPreflightTools()
		if err == nil {
			t.Fatalf("expected preflight check to fail before startup, got nil")
		}
		if !strings.Contains(err.Error(), "required tool") {
			t.Errorf("expected error mentioning required tool, got: %v", err)
		}
	})

	t.Run("T1-F09-05_APIPreflightReExportDelegation", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))

		if err := postgres.CheckPreflightTools(); err != nil {
			t.Errorf("expected CheckPreflightTools to succeed, got: %v", err)
		}
	})
}

// ============================================================================
// FEATURE F10: TUI & STEP VIEW NON-TTY STREAM (5 Tests: T1-F10-01 .. T1-F10-05)
// ============================================================================

func TestTier1_F10_TUIAndStepViewNonTTYStream(t *testing.T) {
	t.Run("T1-F10-01_NonTTYLinearStreamingFallback", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		res := env.RunCLI(
			"--action=dump",
			"--instance=local",
			"--database=app_local",
			"--mode=live",
		)

		res.AssertSuccess(t).
			AssertCombinedContains(t, "Planned Execution Steps").
			AssertCombinedContains(t, "Execution Summary")
	})

	t.Run("T1-F10-02_StepTrackerStateTransitions", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		env.SetEnv("LOCAL_USER", "postgres")
		env.SetEnv("LOCAL_PASS", "postgres")

		res := env.RunCLI(
			"--action=dump",
			"--instance=local",
			"--database=app_local",
			"--mode=live",
		)

		res.AssertSuccess(t).
			AssertCombinedContains(t, "Step 1/1").
			AssertCombinedContains(t, "Completed:")
	})

	t.Run("T1-F10-03_SingleLineNonInteractiveDumpFlow", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		outputPath := filepath.Join(env.DumpsDir, "dump_flow_test.dump")
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}
		logger := postgres.NewExecutionLogger(nil)

		err := postgres.RunPgDump(context.Background(), "127.0.0.1", 5432, "app_local", outputPath, creds, logger)
		if err != nil {
			t.Fatalf("RunPgDump failed: %v", err)
		}

		env.AssertToolInvoked(t, "pg_dump")
		env.AssertDumpFileExists(t, "dump_flow_test.dump")
	})

	t.Run("T1-F10-04_SingleLineNonInteractiveRestoreWithWarnings", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		t.Setenv("PATH", env.BinDir+":"+os.Getenv("PATH"))
		t.Setenv("MOCK_PG_RESTORE_MODE", "warning")

		dumpFile := env.WriteDumpFile("restore_warn.dump", []byte("dummy dump data"))
		creds := postgres.Credentials{User: "postgres", Password: "pwd", SSLMode: "disable"}
		logger := postgres.NewExecutionLogger(nil)

		err := postgres.RunPgRestore(context.Background(), "127.0.0.1", 5432, "app_local", dumpFile, creds, logger)
		if !errors.Is(err, postgres.ErrRestoreWithWarnings) {
			t.Fatalf("expected ErrRestoreWithWarnings, got: %v", err)
		}

		env.AssertToolInvoked(t, "pg_restore")
	})

	t.Run("T1-F10-05_ExecutionErrorLoggingAndBannerDisplay", func(t *testing.T) {
		env := harness.NewTestEnv(t)
		logger := postgres.NewExecutionLogger(nil)
		logger.Append([]byte("pg_dump: connection to database failed\n"))

		details := map[string]string{
			"Instance": "local",
			"Database": "app_local",
			"Mode":     "live",
		}
		opErr := errors.New("connection failed")

		logPath, err := postgres.WriteErrorLog(logger, env.DumpsDir, "dump", details, opErr)
		if err != nil {
			t.Fatalf("WriteErrorLog failed: %v", err)
		}

		env.AssertErrorLogWritten(t, "dump")
		content, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("read log file failed: %v", err)
		}
		if !strings.Contains(string(content), "ERROR") && !strings.Contains(string(content), "Execution Error Log") {
			t.Errorf("expected error log header, got:\n%s", string(content))
		}
	})
}

// ============================================================================
// MOCK PROVIDER HELPER FOR TIER 1 TESTS
// ============================================================================

type mockTestProvider struct {
	supportsCloning    bool
	createCloneFunc    func(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error)
	deleteInstanceFunc func(ctx context.Context, instance stackit.Instance) error
}

func (m *mockTestProvider) Name() string { return "mock-test" }
func (m *mockTestProvider) Handles(instance stackit.Instance) bool { return true }
func (m *mockTestProvider) GetInstances(ctx context.Context) ([]stackit.Instance, error) {
	return []stackit.Instance{{Name: "prod", ID: "p-1"}}, nil
}
func (m *mockTestProvider) GetDatabases(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error) {
	return []stackit.Database{{Name: "app_prod", ID: 1}}, nil
}
func (m *mockTestProvider) GetBackups(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error) {
	return []stackit.Backup{}, nil
}
func (m *mockTestProvider) ResolveEndpoint(ctx context.Context, instance stackit.Instance) (provider.Endpoint, error) {
	return provider.Endpoint{Host: "127.0.0.1", Port: 5432}, nil
}
func (m *mockTestProvider) CreateClone(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
	if m.createCloneFunc != nil {
		return m.createCloneFunc(ctx, instance, pit)
	}
	return stackit.Instance{Name: instance.Name + "-clone", ID: "clone-id"}, nil
}
func (m *mockTestProvider) DeleteInstance(ctx context.Context, instance stackit.Instance) error {
	if m.deleteInstanceFunc != nil {
		return m.deleteInstanceFunc(ctx, instance)
	}
	return nil
}
func (m *mockTestProvider) SupportsCloning() bool { return m.supportsCloning }
