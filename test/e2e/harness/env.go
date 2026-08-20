package harness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RunResult encapsulates the outcome of a CLI invocation.
type RunResult struct {
	ExitCode       int
	Stdout         string
	Stderr         string
	CombinedOutput string
	Err            error
	Duration       time.Duration
	Args           []string
}

// TestEnv manages an isolated sandbox for end-to-end testing.
type TestEnv struct {
	T              testing.TB
	RootDir        string
	BinDir         string
	WorkspaceDir   string
	DumpsDir       string
	InvocationsLog string
	BinaryPath     string
	Env            map[string]string
}

// NewTestEnv creates a fresh hermetic test environment with mocked postgres tools.
func NewTestEnv(t testing.TB) *TestEnv {
	t.Helper()

	rootDir, err := os.MkdirTemp("", "stackit-restore-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temporary sandbox directory: %v", err)
	}

	binDir := filepath.Join(rootDir, "bin")
	workspaceDir := filepath.Join(rootDir, "workspace")
	dumpsDir := filepath.Join(rootDir, "dumps")
	invocationsLog := filepath.Join(binDir, "invocations.log")

	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin directory: %v", err)
	}
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace directory: %v", err)
	}
	if err := os.MkdirAll(dumpsDir, 0755); err != nil {
		t.Fatalf("Failed to create dumps directory: %v", err)
	}

	// Setup mock tools
	if err := SetupMockTools(binDir); err != nil {
		t.Fatalf("Failed to setup mock tools in %s: %v", binDir, err)
	}

	// Compile binary once and cache
	binPath := GetBinaryPath(t)

	// Clean, sanitized base environment
	hostPath := os.Getenv("PATH")
	if hostPath == "" {
		hostPath = "/usr/local/bin:/usr/bin:/bin"
	}
	sanitizedPath := binDir + ":" + hostPath

	envMap := map[string]string{
		"PATH":              sanitizedPath,
		"TMPDIR":            rootDir,
		"HOME":              rootDir,
		"USER":              os.Getenv("USER"),
		"LANG":              "C.UTF-8",
		"LC_ALL":            "C.UTF-8",
		"PWD":               workspaceDir,
		"POSTGRES_DUMP_DIR": dumpsDir,
		"MOCK_PG_LOG":       invocationsLog,
	}

	env := &TestEnv{
		T:              t,
		RootDir:        rootDir,
		BinDir:         binDir,
		WorkspaceDir:   workspaceDir,
		DumpsDir:       dumpsDir,
		InvocationsLog: invocationsLog,
		BinaryPath:     binPath,
		Env:            envMap,
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(rootDir)
	})

	return env
}

// SetEnv sets or overrides an environment variable in the sandbox.
func (e *TestEnv) SetEnv(key, value string) *TestEnv {
	e.Env[key] = value
	return e
}

// UnsetEnv removes an environment variable from the sandbox.
func (e *TestEnv) UnsetEnv(key string) *TestEnv {
	delete(e.Env, key)
	return e
}

// WriteWorkspaceFile writes a file inside the workspace directory and returns its absolute path.
func (e *TestEnv) WriteWorkspaceFile(filename, content string) string {
	e.T.Helper()
	filePath := filepath.Join(e.WorkspaceDir, filename)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		e.T.Fatalf("Failed to create parent dir for %s: %v", filePath, err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		e.T.Fatalf("Failed to write workspace file %s: %v", filePath, err)
	}
	return filePath
}

// WriteDotEnv writes a .env file into the workspace with the provided key-values.
func (e *TestEnv) WriteDotEnv(vars map[string]string) string {
	e.T.Helper()
	var buf bytes.Buffer
	for k, v := range vars {
		buf.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}
	return e.WriteWorkspaceFile(".env", buf.String())
}

// WriteDumpFile creates a mock .dump file and optional sidecar .dump.json inside dumpsDir.
func (e *TestEnv) WriteDumpFile(filename string, payload []byte) string {
	e.T.Helper()
	filePath := filepath.Join(e.DumpsDir, filename)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		e.T.Fatalf("Failed to create parent dir for dump file %s: %v", filePath, err)
	}
	if err := os.WriteFile(filePath, payload, 0600); err != nil {
		e.T.Fatalf("Failed to write dump file %s: %v", filePath, err)
	}
	return filePath
}

// RunCLI executes the stackit-restore binary with the provided arguments.
func (e *TestEnv) RunCLI(args ...string) *RunResult {
	return e.RunCLIWithEnvAndStdin(nil, "", args...)
}

// RunCLIWithEnv executes the CLI with additional per-invocation environment overrides.
func (e *TestEnv) RunCLIWithEnv(extraEnv map[string]string, args ...string) *RunResult {
	return e.RunCLIWithEnvAndStdin(extraEnv, "", args...)
}

// RunCLIWithStdin executes the CLI with standard input provided.
func (e *TestEnv) RunCLIWithStdin(stdin string, args ...string) *RunResult {
	return e.RunCLIWithEnvAndStdin(nil, stdin, args...)
}

// RunCLIWithEnvAndStdin executes the CLI with custom environment overrides and stdin.
func (e *TestEnv) RunCLIWithEnvAndStdin(extraEnv map[string]string, stdin string, args ...string) *RunResult {
	e.T.Helper()

	cmd := exec.Command(e.BinaryPath, args...)
	cmd.Dir = e.WorkspaceDir

	// Assemble environment slice
	envMap := make(map[string]string, len(e.Env)+len(extraEnv))
	for k, v := range e.Env {
		envMap[k] = v
	}
	for k, v := range extraEnv {
		envMap[k] = v
	}

	envSlice := make([]string, 0, len(envMap))
	for k, v := range envMap {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = envSlice

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()
	combinedStr := stdoutStr + stderrStr

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return &RunResult{
		ExitCode:       exitCode,
		Stdout:         stdoutStr,
		Stderr:         stderrStr,
		CombinedOutput: combinedStr,
		Err:            err,
		Duration:       duration,
		Args:           args,
	}
}

// GetInvocations returns all tool invocations recorded in this test environment.
func (e *TestEnv) GetInvocations() ([]InvocationRecord, error) {
	return ReadInvocations(e.InvocationsLog)
}

// ClearInvocations resets the invocations log.
func (e *TestEnv) ClearInvocations() error {
	return ClearInvocations(e.InvocationsLog)
}

// ListDumpFiles returns all files ending in .dump inside the dumps directory.
func (e *TestEnv) ListDumpFiles() ([]string, error) {
	entries, err := os.ReadDir(e.DumpsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dumps []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".dump") {
			dumps = append(dumps, filepath.Join(e.DumpsDir, entry.Name()))
		}
	}
	return dumps, nil
}

// ListErrorLogs returns all error log files inside <dumps>/logs/.
func (e *TestEnv) ListErrorLogs() ([]string, error) {
	logsDir := filepath.Join(e.DumpsDir, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var logs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "error_") && strings.HasSuffix(entry.Name(), ".log") {
			logs = append(logs, filepath.Join(logsDir, entry.Name()))
		}
	}
	return logs, nil
}
