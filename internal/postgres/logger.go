package postgres

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	executionLogMu  sync.Mutex
	executionBuffer bytes.Buffer
)

func ResetExecutionBuffer() {
	executionLogMu.Lock()
	defer executionLogMu.Unlock()
	executionBuffer.Reset()
}

func AppendExecutionLog(data []byte) {
	executionLogMu.Lock()
	defer executionLogMu.Unlock()
	executionBuffer.Write(data)
}

func GetExecutionLog() string {
	executionLogMu.Lock()
	defer executionLogMu.Unlock()
	return executionBuffer.String()
}

func WriteErrorLog(
	dumpDir string,
	action string,
	details map[string]string,
	err error,
) (string, error) {
	if strings.TrimSpace(dumpDir) == "" {
		dumpDir = "dumps"
	}
	logsDir := filepath.Join(dumpDir, "logs")
	if mkErr := os.MkdirAll(logsDir, 0755); mkErr != nil {
		return "", fmt.Errorf("create logs directory %q: %w", logsDir, mkErr)
	}

	timestamp := time.Now().UTC().Format("20060102_150405")
	cleanAction := strings.ToLower(strings.TrimSpace(action))
	if cleanAction == "" {
		cleanAction = "operation"
	}
	logFileName := fmt.Sprintf("error_%s_%s.log", timestamp, cleanAction)
	logFilePath := filepath.Join(logsDir, logFileName)

	var sb strings.Builder
	sb.WriteString("================================================================================\n")
	sb.WriteString("STACKIT PostgreSQL CLI - Execution Error Log\n")
	sb.WriteString("================================================================================\n")
	sb.WriteString(fmt.Sprintf("Timestamp:   %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Action:      %s\n", action))

	for k, v := range details {
		sb.WriteString(fmt.Sprintf("%-12s %s\n", k+":", v))
	}

	if err != nil {
		sb.WriteString(fmt.Sprintf("Error:       %s\n", err.Error()))
	}
	sb.WriteString("================================================================================\n")
	sb.WriteString("Captured Command Output / Transcript:\n")
	sb.WriteString("================================================================================\n")

	captured := GetExecutionLog()
	if strings.TrimSpace(captured) == "" {
		sb.WriteString("(No stdout/stderr captured during this session)\n")
	} else {
		sb.WriteString(captured)
		if !strings.HasSuffix(captured, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("================================================================================\n")

	if writeErr := os.WriteFile(logFilePath, []byte(sb.String()), 0644); writeErr != nil {
		return "", fmt.Errorf("write error log %q: %w", logFilePath, writeErr)
	}

	return logFilePath, nil
}
