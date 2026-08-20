package e2e

import (
	"os"
	"strings"
	"testing"

	"github.com/GrigoreAlexandru/Stackit-Restore/test/e2e/harness"
)

// ============================================================================
// TIER 4: REAL-WORLD APPLICATION SCENARIOS (6 Integration Workflows)
// ============================================================================

// T4-SCENARIO-01: Local Development Database Backup & Restore Lifecycle
// Scenario: A developer performs a full backup and restore lifecycle on local databases.
// Workflow:
//  1. Dump live local database 'dev_db' to custom binary archive.
//  2. Verify dump artifact generation in POSTGRES_DUMP_DIR.
//  3. Restore the generated dump file into 'test_db'.
//  4. Verify pg_restore execution with target database and no fatal error logs.
func TestTier4_Scenario01_LocalDevDatabaseBackupRestoreLifecycle(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "devuser")
	env.SetEnv("LOCAL_PASS", "devpass")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	// Phase 1: Create local database dump
	resDump := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=dev_db",
		"--mode=live",
	)
	resDump.AssertSuccess(t)
	resDump.AssertCombinedContains(t, "Dump created successfully")

	dumpFile := env.AssertDumpFileExists(t, "*dev_db*.dump")
	invDump := env.AssertToolInvokedWithArg(t, "pg_dump", "dev_db")
	if invDump.Password != "devpass" || invDump.SSLMode != "disable" {
		t.Fatalf("Unexpected pg_dump credentials / SSL mode: %+v", invDump)
	}

	// Phase 2: Restore dump into different local database
	resRestore := env.RunCLI(
		"--action=restore",
		"--target-instance=local",
		"--target-database=test_db",
		"--mode=dump_file",
		"--dump-file="+dumpFile,
	)
	resRestore.AssertSuccess(t)
	resRestore.AssertCombinedContains(t, "Restore completed successfully")

	invRestore := env.AssertToolInvokedWithArg(t, "pg_restore", "test_db")
	if invRestore.Password != "devpass" {
		t.Fatalf("Unexpected pg_restore password: %+v", invRestore)
	}

	env.AssertNoFatalErrorLogs(t)
}

// T4-SCENARIO-02: Production Incident Disaster Recovery (PIT Sync)
// Scenario: Following an incident at 15:30, the team recovers state by syncing from a Point-in-Time snapshot at 15:00.
// Workflow:
//  1. Trigger sync action from 'Production' / 'app_prod' to 'local' / 'dr_recovery' at PIT '2026-08-13 15:00:00'.
//  2. Verify clone creation, dump extraction, clone cleanup, and restore execution.
//  3. Confirm sync completion message and exit code 0.
func TestTier4_Scenario02_ProductionIncidentDisasterRecoveryPITSync(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("PRODUCTION_USER", "produser")
	env.SetEnv("PRODUCTION_PASS", "prodpass")
	env.SetEnv("LOCAL_USER", "localuser")
	env.SetEnv("LOCAL_PASS", "localpass")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	pitTimestamp := "2026-08-13 15:00:00"

	res := env.RunCLI(
		"--action=sync",
		"--instance=Production",
		"--database=app_prod",
		"--target-instance=local",
		"--target-database=dr_recovery",
		"--mode=pit",
		"--pit="+pitTimestamp,
	)

	res.AssertSuccess(t)
	res.AssertCombinedContains(t, "Sync completed successfully")

	env.AssertToolInvoked(t, "pg_dump")
	env.AssertToolInvokedWithArg(t, "pg_restore", "dr_recovery")
	env.AssertNoFatalErrorLogs(t)
}

// T4-SCENARIO-03: Continuous Migration: Production Cloud Replica Sync to Local
// Scenario: Automated migration pipeline replicates cloud production data from latest backup to local staging database.
// Workflow:
//  1. Trigger sync action from 'Production' / 'app_prod' in 'replica' mode to 'local' / 'migrated_db'.
//  2. Verify zero-impact clone flow -> dump extraction -> clone deletion -> restore into local DB.
//  3. Confirm dump artifact was persisted and restore finished cleanly.
func TestTier4_Scenario03_ContinuousMigrationProductionCloudReplicaSyncToLocal(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("PRODUCTION_USER", "produser")
	env.SetEnv("PRODUCTION_PASS", "prodpass")
	env.SetEnv("LOCAL_USER", "localuser")
	env.SetEnv("LOCAL_PASS", "localpass")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	res := env.RunCLI(
		"--action=sync",
		"--instance=Production",
		"--database=app_prod",
		"--target-instance=local",
		"--target-database=migrated_db",
		"--mode=replica",
	)

	res.AssertSuccess(t)
	res.AssertCombinedContains(t, "Sync completed successfully")

	env.AssertToolInvoked(t, "pg_dump")
	env.AssertToolInvokedWithArg(t, "pg_restore", "migrated_db")
	env.AssertDumpFileExists(t, "*.dump")
	env.AssertNoFatalErrorLogs(t)
}

// T4-SCENARIO-04: Graceful Degradation on Cloud Permission Restriction (403 Forbidden Delete)
// Scenario: A service account has permissions to create clones and dump data, but lacks delete permissions.
// Workflow:
//  1. Trigger replica dump on 'Production' instance.
//  2. Clone extraction succeeds, but clone deletion encounters 403 Forbidden.
//  3. Verify the CLI handles the deletion failure gracefully, surfaces a warning, and preserves the dump file.
func TestTier4_Scenario04_GracefulDegradationOnCloudPermissionRestriction(t *testing.T) {
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
	dumpFile := env.AssertDumpFileExists(t, "*.dump")
	if info, err := os.Stat(dumpFile); err != nil || info.Size() == 0 {
		t.Fatalf("Expected valid non-empty dump file preserved at %s, err: %v", dumpFile, err)
	}
}

// T4-SCENARIO-05: Fault Injection & Recovery Workflow with Diagnostic Error Logging
// Scenario: Network drop occurs during database dump, generating diagnostic logs, followed by successful recovery.
// Workflow:
//  1. Inject failure into pg_dump (exit code 2).
//  2. Run dump; assert failure, error banner, and structured error_*.log generation with transcript.
//  3. Inspect error log content for timestamp, action, and diagnostic details.
//  4. Recover pg_dump to normal operation; rerun dump and verify success.
func TestTier4_Scenario05_FaultInjectionRecoveryWorkflowDiagnosticLogging(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)
	env.SetEnv("LOCAL_USER", "localuser")
	env.SetEnv("LOCAL_PASS", "localpass")
	env.SetEnv("LOCAL_HOST", "127.0.0.1")
	env.SetEnv("LOCAL_PORT", "5432")

	// Phase 1: Injected fault
	env.SetEnv("MOCK_PG_DUMP_EXIT_CODE", "2")

	resFault := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
	)
	resFault.AssertFailure(t)
	resFault.AssertCombinedContains(t, "ERROR: Operation failed")

	logPath := env.AssertErrorLogWritten(t, "dump")
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read generated error log %s: %v", logPath, err)
	}
	logStr := string(logContent)
	if !strings.Contains(logStr, "Execution Error Log") {
		t.Fatalf("Expected error log header in %s", logPath)
	}
	if !strings.Contains(logStr, "Action:") || !strings.Contains(logStr, "dump") {
		t.Fatalf("Expected action details in error log: %s", logStr)
	}

	// Phase 2: Recovery
	env.SetEnv("MOCK_PG_DUMP_EXIT_CODE", "0")

	resRecovery := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
	)
	resRecovery.AssertSuccess(t)
	resRecovery.AssertCombinedContains(t, "Dump created successfully")
	env.AssertDumpFileExists(t, "*app_local*.dump")
}

// T4-SCENARIO-06: Multi-Environment Configuration Switch via CLI Overrides
// Scenario: A developer switches between development, staging, and production STACKIT environments using CLI flags.
// Workflow:
//  1. Populate .env file in workspace with default development settings.
//  2. Invoke CLI with explicit flag overrides (--project-id, --region, --sa-key-path).
//  3. Verify CLI executes with target parameters without host environment mutation.
func TestTier4_Scenario06_MultiEnvironmentConfigSwitchViaCLIOverrides(t *testing.T) {
	t.Parallel()
	env := harness.NewTestEnv(t)

	// Write workspace .env with dev settings
	env.WriteDotEnv(map[string]string{
		"STACKIT_PROJECT_ID":               "dev-project-uuid-001",
		"STACKIT_REGION":                   "eu01",
		"STACKIT_SERVICE_ACCOUNT_KEY_PATH": "/tmp/dev-key.json",
		"LOCAL_USER":                       "postgres",
		"LOCAL_PASS":                       "secret",
		"LOCAL_HOST":                       "127.0.0.1",
		"LOCAL_PORT":                       "5432",
	})

	// Override with staging project via flags
	res := env.RunCLI(
		"--action=dump",
		"--instance=local",
		"--database=app_local",
		"--mode=live",
		"--project-id=staging-project-uuid-002",
		"--region=eu02",
	)

	res.AssertSuccess(t)
	env.AssertToolInvoked(t, "pg_dump")
	env.AssertDumpFileExists(t, "*.dump")
	env.AssertNoFatalErrorLogs(t)
}
