package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryCompilation(t *testing.T) {
	binPath := GetBinaryPath(t)
	if binPath == "" {
		t.Fatalf("Expected non-empty binary path")
	}

	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("Failed to stat compiled binary at %s: %v", binPath, err)
	}

	if info.Mode()&0111 == 0 {
		t.Fatalf("Compiled binary is not executable: mode=%v", info.Mode())
	}
}

func TestMockToolsGeneration(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")

	if err := SetupMockTools(binDir); err != nil {
		t.Fatalf("SetupMockTools failed: %v", err)
	}

	pgDumpPath := filepath.Join(binDir, "pg_dump")
	pgRestorePath := filepath.Join(binDir, "pg_restore")

	for _, path := range []string{pgDumpPath, pgRestorePath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Mock tool missing at %s: %v", path, err)
		}
		if info.Mode()&0111 == 0 {
			t.Fatalf("Mock tool %s is not executable: mode=%v", path, info.Mode())
		}
	}
}

func TestMockPgDumpExecution(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := SetupMockTools(binDir); err != nil {
		t.Fatalf("SetupMockTools failed: %v", err)
	}

	logFile := filepath.Join(tempDir, "invocations.log")
	outFile := filepath.Join(tempDir, "dumps", "test.dump")

	// Run pg_dump shim
	cmd := exec.Command(filepath.Join(binDir, "pg_dump"), "--host", "127.0.0.1", "--file", outFile, "--dbname", "testdb")
	cmd.Env = []string{
		"MOCK_PG_LOG=" + logFile,
		"PGPASSWORD=secret",
		"PGSSLMODE=disable",
		"PATH=" + os.Getenv("PATH"),
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pg_dump shim failed: %v, output: %s", err, string(output))
	}

	// Verify dump file written
	if _, err := os.Stat(outFile); err != nil {
		t.Fatalf("Expected dump file %s to be created: %v", outFile, err)
	}
	content, err := os.ReadFile(outFile)
	if err != nil || !strings.Contains(string(content), "PGDMP") {
		t.Fatalf("Unexpected dump payload: %s", string(content))
	}

	// Verify invocation log parsed
	invocations, err := ReadInvocations(logFile)
	if err != nil {
		t.Fatalf("Failed to read invocations: %v", err)
	}
	if len(invocations) != 1 {
		t.Fatalf("Expected 1 invocation, got %d", len(invocations))
	}
	if invocations[0].Tool != "pg_dump" || invocations[0].Password != "secret" || invocations[0].SSLMode != "disable" {
		t.Fatalf("Unexpected invocation record: %+v", invocations[0])
	}
}

func TestMockPgRestoreModes(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := SetupMockTools(binDir); err != nil {
		t.Fatalf("SetupMockTools failed: %v", err)
	}

	pgRestore := filepath.Join(binDir, "pg_restore")

	// Mode: clean -> exit 0
	cmdClean := exec.Command(pgRestore, "--dbname", "testdb")
	cmdClean.Env = []string{"MOCK_PG_RESTORE_MODE=clean", "PATH=" + os.Getenv("PATH")}
	if err := cmdClean.Run(); err != nil {
		t.Fatalf("Expected clean mode to exit 0, got %v", err)
	}

	// Mode: warning -> exit 1 with warning on stderr
	cmdWarning := exec.Command(pgRestore, "--dbname", "testdb")
	cmdWarning.Env = []string{"MOCK_PG_RESTORE_MODE=warning", "PATH=" + os.Getenv("PATH")}
	warningOut, err := cmdWarning.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected warning mode to exit non-zero")
	}
	if !strings.Contains(string(warningOut), "pg_stat_kcache") || !strings.Contains(string(warningOut), "errors were ignored") {
		t.Fatalf("Expected warning output, got: %s", string(warningOut))
	}

	// Mode: fatal -> exit 1 with fatal connection error
	cmdFatal := exec.Command(pgRestore, "--dbname", "testdb")
	cmdFatal.Env = []string{"MOCK_PG_RESTORE_MODE=fatal", "PATH=" + os.Getenv("PATH")}
	fatalOut, err := cmdFatal.CombinedOutput()
	if err == nil {
		t.Fatalf("Expected fatal mode to exit non-zero")
	}
	if !strings.Contains(string(fatalOut), "connection to server failed") {
		t.Fatalf("Expected fatal connection error, got: %s", string(fatalOut))
	}
}

func TestTestEnvSandboxAndAssertions(t *testing.T) {
	env := NewTestEnv(t)

	// Test CLI help
	res := env.RunCLI("--help")
	res.AssertSuccess(t)
	res.AssertStdoutContains(t, "PostgreSQL Restore CLI")
	res.AssertStdoutMatches(t, `(?i)usage`)
	res.AssertStderrNotContains(t, "error")

	// Test CLI short help
	resShort := env.RunCLI("-h")
	resShort.AssertSuccess(t)
	resShort.AssertExitCode(t, 0)
}
