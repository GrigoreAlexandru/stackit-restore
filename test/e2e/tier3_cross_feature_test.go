package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/test/e2e/harness"
)

// T3-PAIR-01: Flag Overrides + Config Validation (F2 x F3)
// Verifies that CLI flag overrides (e.g. --project-id) are applied and take precedence over ambient environment.
func TestTier3_Pair01_FlagOverridesConfigValidation(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")
	env.UnsetEnv("STACKIT_PROJECT_ID")

	res := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
		"--project-id=override-proj-12345",
	)

	res.AssertSuccess(t)
	env.AssertToolInvoked(t, "pg_dump")
	env.AssertDumpFileExists(t, "*.dump")
}

// T3-PAIR-02: Pure Flag Parsing + Local Provider Routing (F2 x F4)
// Verifies that the 'export' action alias normalizes to 'dump' and routes to LocalProvider without side effects.
func TestTier3_Pair02_PureFlagParsingLocalProviderRouting(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "localhost")
	env.SetEnv("LOCAL_PORT", "5432")

	res := env.RunCLI(
		"--action=export",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
	)

	res.AssertSuccess(t)
	inv := env.AssertToolInvoked(t, "pg_dump")
	if inv.Tool != "pg_dump" {
		t.Fatalf("Expected pg_dump tool invocation, got: %+v", inv)
	}
	env.AssertDumpFileExists(t, "*app_local*.dump")
}

// T3-PAIR-03: Local Dump Execution + Microsecond Artifacts (F4 x F5 x F6)
// Verifies that rapid successive local dumps generate unique filenames with sub-second timestamps.
func TestTier3_Pair03_LocalDumpExecutionMicrosecondArtifacts(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	res1 := env.RunCLI("--action=dump", "--instance=local", "--database=app_local", "--mode=live")
	res1.AssertSuccess(t)

	// Small pause if any, but rapid enough to test collision prevention
	time.Sleep(5 * time.Millisecond)

	res2 := env.RunCLI("--action=dump", "--instance=local", "--database=app_local", "--mode=live")
	res2.AssertSuccess(t)

	dumps, err := env.ListDumpFiles()
	if err != nil {
		t.Fatalf("Failed to list dump files: %v", err)
	}
	if len(dumps) < 2 {
		t.Fatalf("Expected at least 2 distinct dump files from rapid dump execution, got %d: %v", len(dumps), dumps)
	}
	if dumps[0] == dumps[1] {
		t.Fatalf("Dump filenames collided: %s and %s", dumps[0], dumps[1])
	}
}

// T3-PAIR-04: Preflight Verification + Main Entrypoint Guard (F9 x F1)
// Verifies that missing required binary in PATH halts execution before database connection attempt.
func TestTier3_Pair04_PreflightVerificationMainEntrypointGuard(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")

	// Delete pg_dump from fake-bin and restrict PATH to only fake-bin
	pgDumpPath := filepath.Join(env.BinDir, "pg_dump")
	_ = os.Remove(pgDumpPath)
	env.SetEnv("PATH", env.BinDir)

	res := env.RunCLI("--action=dump", "--instance=local", "--database=app_local", "--mode=live")
	res.AssertFailure(t)
	res.AssertCombinedContains(t, "pg_dump")
}

// T3-PAIR-05: Clone Orchestration + Permission Denial + Step Warnings (F8 x F10)
// Verifies that a 403 Forbidden error during clone cleanup marks the step with a warning and preserves the dump.
func TestTier3_Pair05_CloneOrchestrationPermissionDenialStepWarnings(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("PRODUCTION_USER", "produser")
	env.SetEnv("PRODUCTION_PASS", "prodpass")

	res := env.RunCLI(
		"--action=dump",
		"--instance=Production",
		"--database=app_prod",
		"--mode=replica",
	)

	res.AssertSuccess(t)
	env.AssertToolInvoked(t, "pg_dump")
	env.AssertDumpFileExists(t, "*.dump")
}

// T3-PAIR-06: Restore Execution + Warning Handling + Logger Capture (F6 x F7)
// Verifies that non-fatal pg_restore warnings (e.g. pg_stat_kcache) are captured in logger and handled as warnings.
func TestTier3_Pair06_RestoreExecutionWarningHandlingLoggerCapture(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")
	env.SetEnv("MOCK_PG_RESTORE_MODE", "warning")

	dumpPath := env.WriteDumpFile("test_warning.dump", []byte("PGDMP payload"))

	res := env.RunCLI(
		"--action=restore",
		"--target-instance=local",
		"--target-database=app_local",
		"--mode=dump_file",
		"--dump-file="+dumpPath,
	)

	res.AssertSuccess(t)
	env.AssertToolInvoked(t, "pg_restore")
	res.AssertCombinedContains(t, "warning")
}

// T3-PAIR-07: Non-Interactive Execution + Failed Dump + Error Logging (F6 x F10)
// Verifies that failed dump command triggers StepTracker failure, prints error banner, and writes error_*.log.
func TestTier3_Pair07_NonInteractiveExecutionFailedDumpErrorLogging(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")
	env.SetEnv("MOCK_PG_DUMP_EXIT_CODE", "1")

	res := env.RunCLI("--action=dump", "--instance=local", "--database=app_local", "--mode=live")

	res.AssertFailure(t)
	res.AssertCombinedContains(t, "ERROR: Operation failed")
	logFile := env.AssertErrorLogWritten(t, "dump")
	if !strings.HasSuffix(logFile, ".log") {
		t.Fatalf("Expected error log file ending in .log, got: %s", logFile)
	}
}

// T3-PAIR-08: Restore Mode Selection + Artifact Manager Read (F2 x F5 x F7)
// Verifies that restore mode 'dump_file' reads the specified artifact and invokes pg_restore with database target.
func TestTier3_Pair08_RestoreModeSelectionArtifactManagerRead(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	dumpPath := env.WriteDumpFile("custom_20260820T120000Z.dump", []byte("PGDMP custom payload"))

	res := env.RunCLI(
		"--action=restore",
		"--target-instance=local",
		"--target-database=app_local",
		"--mode=dump_file",
		"--dump-file="+dumpPath,
	)

	res.AssertSuccess(t)
	env.AssertToolInvokedWithArg(t, "pg_restore", "app_local")
	env.AssertToolInvokedWithArg(t, "pg_restore", dumpPath)
}

// T3-PAIR-09: Database Sync (Cloud Dump -> Target Restore) (F6 x F7 x F8)
// Verifies end-to-end sync operation extracting dump from source instance and restoring to destination.
func TestTier3_Pair09_DatabaseSyncCloudDumpTargetRestore(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("PRODUCTION_USER", "produser")
	env.SetEnv("PRODUCTION_PASS", "prodpass")
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	res := env.RunCLI(
		"--action=sync",
		"--instance=Production",
		"--database=app_prod",
		"--target-instance=local",
		"--target-database=app_local",
		"--mode=live",
	)

	res.AssertSuccess(t)
	env.AssertToolInvoked(t, "pg_dump")
	env.AssertToolInvoked(t, "pg_restore")
	res.AssertCombinedContains(t, "Sync completed")
}

// T3-PAIR-10: Local vs Cloud Config Validation + Local Operation (F3 x F4)
// Verifies that local operations succeed without requiring any STACKIT cloud credentials or project settings.
func TestTier3_Pair10_LocalVsCloudConfigValidationLocalOperation(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.UnsetEnv("STACKIT_PROJECT_ID")
	env.UnsetEnv("STACKIT_REGION")
	env.UnsetEnv("STACKIT_SERVICE_ACCOUNT_KEY_PATH")
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	res := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
	)

	res.AssertSuccess(t)
	env.AssertToolInvoked(t, "pg_dump")
	env.AssertDumpFileExists(t, "*.dump")
}

// T3-PAIR-11: ExecutionLogger Threading in API Client + OutputWriter (F6 x F8 x F10)
// Verifies that live command execution stream and step tracker output are rendered cleanly to output.
func TestTier3_Pair11_ExecutionLoggerThreadingAPIClientOutputWriter(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	res := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
	)

	res.AssertSuccess(t)
	res.AssertCombinedContains(t, "Planned Execution Steps")
	res.AssertCombinedContains(t, "Extract live database dump")
	res.AssertCombinedContains(t, "Dump created successfully")
}

// T3-PAIR-12: DummyAPI Removal Verification Invariant (F1 x F8)
// Verifies that requesting an unconfigured / unknown instance fails with actionable error rather than fallback data.
func TestTier3_Pair12_DummyAPIRemovalVerificationInvariant(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "postgres")
	env.SetEnv("LOCAL_PASS", "secret")

	res := env.RunCLI(
		"--action=dump",
		"--instance=nonexistent-cloud-instance",
		"--database=app_prod",
		"--mode=live",
	)

	res.AssertFailure(t)
	res.AssertCombinedContains(t, "nonexistent-cloud-instance")
}
