package harness

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InvocationRecord captures the metadata of a mocked command invocation.
type InvocationRecord struct {
	Timestamp string
	Tool      string
	Password  string
	SSLMode   string
	Args      []string
	RawLine   string
}

const pgDumpScript = `#!/bin/sh
set -e

LOG_FILE="${MOCK_PG_LOG:-"$(dirname "$0")/invocations.log"}"
TIMESTAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

# Record invocation
echo "INVOCATION|${TIMESTAMP}|pg_dump|${PGPASSWORD}|${PGSSLMODE}|$*" >> "$LOG_FILE"

# Parse target dump file from arguments
DUMP_FILE=""
PREV=""
for arg in "$@"; do
    case "$arg" in
        --file=*)
            DUMP_FILE="${arg#--file=}"
            ;;
        -f=*)
            DUMP_FILE="${arg#-f=}"
            ;;
        *)
            if [ "$PREV" = "--file" ] || [ "$PREV" = "-f" ]; then
                DUMP_FILE="$arg"
            fi
            ;;
    esac
    PREV="$arg"
done

# If output file specified, write dummy dump content
if [ -n "$DUMP_FILE" ]; then
    mkdir -p "$(dirname "$DUMP_FILE")"
    printf "PGDMP dummy dump archive payload\n" > "$DUMP_FILE"
fi

# Exit code handling
EXIT_CODE="${MOCK_PG_DUMP_EXIT_CODE:-0}"
if [ "$EXIT_CODE" -ne 0 ]; then
    echo "pg_dump: error: failed to dump database (exit code $EXIT_CODE)" >&2
    exit "$EXIT_CODE"
fi

echo "pg_dump: reading schemas"
echo "pg_dump: saving database definition"
echo "pg_dump: finished dump successfully"
exit 0
`

const pgRestoreScript = `#!/bin/sh

LOG_FILE="${MOCK_PG_LOG:-"$(dirname "$0")/invocations.log"}"
TIMESTAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

# Record invocation
echo "INVOCATION|${TIMESTAMP}|pg_restore|${PGPASSWORD}|${PGSSLMODE}|$*" >> "$LOG_FILE"

# Explicit exit code override takes precedence if set
if [ -n "$MOCK_PG_RESTORE_EXIT_CODE" ] && [ "$MOCK_PG_RESTORE_EXIT_CODE" -ne 0 ]; then
    echo "pg_restore: error: execution failed with exit code $MOCK_PG_RESTORE_EXIT_CODE" >&2
    exit "$MOCK_PG_RESTORE_EXIT_CODE"
fi

# Restore mode handling
RESTORE_MODE="${MOCK_PG_RESTORE_MODE:-clean}"
case "$RESTORE_MODE" in
    warning)
        echo "pg_restore: processing data for table public.users"
        echo "ERROR: extension \"pg_stat_kcache\" is not available" >&2
        echo "warning: errors were ignored during processing" >&2
        exit 1
        ;;
    fatal)
        echo "pg_restore: error: connection to server failed: could not connect to server: Connection refused" >&2
        exit 1
        ;;
    *)
        echo "pg_restore: connecting to database for restore"
        echo "pg_restore: processing items in archive"
        echo "pg_restore: restore completed successfully"
        exit 0
        ;;
esac
`

// SetupMockTools creates executable pg_dump and pg_restore shims inside binDir.
func SetupMockTools(binDir string) error {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("create fake-bin directory %s: %w", binDir, err)
	}

	pgDumpPath := filepath.Join(binDir, "pg_dump")
	if err := os.WriteFile(pgDumpPath, []byte(pgDumpScript), 0755); err != nil {
		return fmt.Errorf("write fake pg_dump to %s: %w", pgDumpPath, err)
	}

	pgRestorePath := filepath.Join(binDir, "pg_restore")
	if err := os.WriteFile(pgRestorePath, []byte(pgRestoreScript), 0755); err != nil {
		return fmt.Errorf("write fake pg_restore to %s: %w", pgRestorePath, err)
	}

	return nil
}

// ReadInvocations reads and parses all recorded tool invocations from logPath.
func ReadInvocations(logPath string) ([]InvocationRecord, error) {
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open invocations log %s: %w", logPath, err)
	}
	defer file.Close()

	var records []InvocationRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "INVOCATION|") {
			continue
		}

		parts := strings.SplitN(line, "|", 6)
		if len(parts) < 6 {
			continue
		}

		rec := InvocationRecord{
			Timestamp: parts[1],
			Tool:      parts[2],
			Password:  parts[3],
			SSLMode:   parts[4],
			Args:      strings.Fields(parts[5]),
			RawLine:   line,
		}
		records = append(records, rec)
	}

	if err := scanner.Err(); err != nil {
		return records, fmt.Errorf("scan invocations log %s: %w", logPath, err)
	}

	return records, nil
}

// ClearInvocations removes or empties the invocations log file.
func ClearInvocations(logPath string) error {
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear invocations log %s: %w", logPath, err)
	}
	return nil
}
