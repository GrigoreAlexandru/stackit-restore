package postgres

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteErrorLog(t *testing.T) {
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")

	ResetExecutionBuffer()
	AppendExecutionLog([]byte("$ pg_restore --host 127.0.0.1\npg_restore: error: connection refused\n"))

	details := map[string]string{
		"Instance": "Production",
		"Database": "app_prod",
	}

	err := errors.New("exit status 1")
	logPath, writeErr := WriteErrorLog(dumpDir, "restore", details, err)
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
