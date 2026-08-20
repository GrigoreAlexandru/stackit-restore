package postgres

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteErrorLog(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	logger := NewExecutionLogger(nil)
	logger.Append([]byte("$ pg_restore --host 127.0.0.1\npg_restore: error: connection refused\n"))

	details := map[string]string{
		"Instance": "Production",
		"Database": "app_prod",
	}

	err := errors.New("exit status 1")
	logPath, writeErr := WriteErrorLog(logger, dumpDir, "restore", details, err)
	if writeErr != nil {
		t.Fatalf("WriteErrorLog failed: %v", writeErr)
	}

	if !strings.HasPrefix(filepath.Dir(logPath), filepath.Join(dumpDir, "logs")) {
		t.Errorf("expected log file in %s/logs, got: %s", dumpDir, logPath)
	}

	content, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("failed to read created log file: %v", readErr)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "STACKIT PostgreSQL CLI - Execution Error Log") {
		t.Errorf("expected header in log file, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "Action:      restore") {
		t.Errorf("expected action restore in log file, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "Instance:    Production") {
		t.Errorf("expected instance Production in log file, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "pg_restore: error: connection refused") {
		t.Errorf("expected captured command output in log file, got:\n%s", contentStr)
	}
}

func TestWriteErrorLog_NilLogger(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	details := map[string]string{
		"Instance": "Staging",
	}

	err := errors.New("timeout connecting")
	logPath, writeErr := WriteErrorLog(nil, dumpDir, "backup", details, err)
	if writeErr != nil {
		t.Fatalf("WriteErrorLog with nil logger failed: %v", writeErr)
	}

	content, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("failed to read created log file: %v", readErr)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "(No stdout/stderr captured during this session)") {
		t.Errorf("expected fallback text when logger is nil, got:\n%s", contentStr)
	}
}

func TestWriteErrorLog_DefaultDumpDir(t *testing.T) {
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current wd: %v", err)
	}
	defer func() {
		_ = os.Chdir(origWd)
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("failed to chdir to tempDir: %v", err)
	}

	logger := NewExecutionLogger(nil)
	logger.Append([]byte("sample log\n"))

	logPath, writeErr := WriteErrorLog(logger, "", "test_action", map[string]string{}, nil)
	if writeErr != nil {
		t.Fatalf("WriteErrorLog with empty dumpDir failed: %v", writeErr)
	}

	expectedPrefix := filepath.Join("dumps", "logs")
	if !strings.Contains(logPath, expectedPrefix) {
		t.Errorf("expected path to contain %q, got %q", expectedPrefix, logPath)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("expected error log file to exist at %q: %v", logPath, err)
	}
}

func TestExecutionLogger_BasicOperations(t *testing.T) {
	var buf bytes.Buffer
	logger := NewExecutionLogger(&buf)

	if logger.GetLog() != "" {
		t.Fatalf("expected empty initial log, got %q", logger.GetLog())
	}
	if logger.GetWriter() != &buf {
		t.Fatalf("expected custom writer, got %v", logger.GetWriter())
	}

	logger.Append([]byte("line 1\n"))
	logger.Append([]byte("line 2\n"))

	if got := logger.GetLog(); got != "line 1\nline 2\n" {
		t.Fatalf("expected concatenated log, got %q", got)
	}

	n, err := logger.Write([]byte("line 3\n"))
	if err != nil || n != 7 {
		t.Fatalf("Write() = (%d, %v), want (7, nil)", n, err)
	}
	if got := logger.GetLog(); got != "line 1\nline 2\nline 3\n" {
		t.Fatalf("expected log with line 3, got %q", got)
	}

	logger.Reset()
	if got := logger.GetLog(); got != "" {
		t.Fatalf("expected empty log after reset, got %q", got)
	}

	// Test SetWriter
	var buf2 bytes.Buffer
	logger.SetWriter(&buf2)
	if logger.GetWriter() != &buf2 {
		t.Fatalf("expected writer buf2, got %v", logger.GetWriter())
	}

	logger.SetWriter(nil)
	if logger.GetWriter() != os.Stdout {
		t.Fatalf("expected os.Stdout fallback when writer is nil, got %v", logger.GetWriter())
	}
}

func TestExecutionLogger_NilSafety(t *testing.T) {
	var nilLogger *ExecutionLogger

	// None of these should panic
	nilLogger.Append([]byte("hello"))
	if log := nilLogger.GetLog(); log != "" {
		t.Fatalf("expected empty string on nil logger GetLog, got %q", log)
	}
	nilLogger.Reset()
	nilLogger.SetWriter(nil)
	if w := nilLogger.GetWriter(); w != os.Stdout {
		t.Fatalf("expected os.Stdout on nil logger GetWriter, got %v", w)
	}
	n, err := nilLogger.Write([]byte("test"))
	if err != nil || n != 4 {
		t.Fatalf("Write on nil logger = (%d, %v), want (4, nil)", n, err)
	}
}

func TestExecutionLogger_ConcurrentAccess(t *testing.T) {
	logger := NewExecutionLogger(nil)
	const numGoroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch j % 5 {
				case 0:
					logger.Append([]byte(fmt.Sprintf("g%d-iter%d\n", id, j)))
				case 1:
					_ = logger.GetLog()
				case 2:
					_ = logger.GetWriter()
				case 3:
					if j%20 == 0 {
						logger.SetWriter(os.Stderr)
					} else {
						logger.SetWriter(nil)
					}
				case 4:
					if j%50 == 0 {
						logger.Reset()
					}
				}
			}
		}(i)
	}

	wg.Wait()
}
