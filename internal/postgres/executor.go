package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var ErrRestoreWithWarnings = errors.New("restore completed with non-fatal warnings")

func IsIgnorableRestoreWarning(output string) bool {
	fatalKeywords := []string{
		"could not connect to server",
		"connection to server at",
		"password authentication failed",
		"FATAL:",
		"PANIC:",
		"out of memory",
		"input file appears to be a text format dump",
		"archiver (db) connection to database",
	}
	lower := strings.ToLower(output)
	for _, kw := range fatalKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return false
		}
	}

	warningPatterns := []string{
		"is not available",
		"could not open extension control file",
		"permission denied to create extension",
		"role \"",
		"schema \"",
		"already exists",
		"does not exist, skipping",
		"extension \"",
		"warnings were ignored during processing",
		"errors were ignored during processing",
	}
	for _, wp := range warningPatterns {
		if strings.Contains(lower, strings.ToLower(wp)) {
			return true
		}
	}
	return false
}

type Credentials struct {
	User     string
	Password string
	SSLMode  string
}

func CheckPreflightTools() error {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return fmt.Errorf("required tool 'pg_dump' not found in PATH: please install postgresql-client")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		return fmt.Errorf("required tool 'pg_restore' not found in PATH: please install postgresql-client")
	}
	return nil
}

var nonAlphaNumRegex = regexp.MustCompile(`[^A-Za-z0-9]+`)

func SanitizeInstanceName(name string) string {
	sanitized := nonAlphaNumRegex.ReplaceAllString(name, "_")
	sanitized = strings.Trim(sanitized, "_")
	return strings.ToUpper(sanitized)
}

func ResolveCredentials(instanceName string) (Credentials, error) {
	if strings.EqualFold(strings.TrimSpace(instanceName), "local") {
		user := os.Getenv("LOCAL_USER")
		pass := os.Getenv("LOCAL_PASS")

		if strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "" {
			return Credentials{}, fmt.Errorf(
				"missing LOCAL_USER or LOCAL_PASS for local database connection",
			)
		}

		sslMode := os.Getenv("LOCAL_SSLMODE")
		if strings.TrimSpace(sslMode) == "" {
			sslMode = "disable"
		}

		return Credentials{
			User:     user,
			Password: pass,
			SSLMode:  sslMode,
		}, nil
	}

	key := SanitizeInstanceName(instanceName)
	if key == "" {
		return Credentials{}, fmt.Errorf("invalid empty instance name for credential resolution")
	}

	userVar := key + "_USER"
	passVar := key + "_PASS"
	user := os.Getenv(userVar)
	pass := os.Getenv(passVar)

	if strings.TrimSpace(user) == "" || strings.TrimSpace(pass) == "" {
		return Credentials{}, fmt.Errorf(
			"missing %s or %s environment variables for instance %q",
			userVar,
			passVar,
			instanceName,
		)
	}

	return Credentials{
		User:     user,
		Password: pass,
		SSLMode:  "require",
	}, nil
}

func HasCredentials(instanceName string) bool {
	_, err := ResolveCredentials(instanceName)
	return err == nil
}

func GetMissingCredentialsHint(instanceName string) string {
	if strings.EqualFold(strings.TrimSpace(instanceName), "local") {
		return "LOCAL_USER and LOCAL_PASS"
	}
	key := SanitizeInstanceName(instanceName)
	return fmt.Sprintf("%s_USER and %s_PASS", key, key)
}

func BuildInstanceHost(instanceID, region string) string {
	return fmt.Sprintf("%s.postgresql.%s.onstackit.cloud", strings.TrimSpace(instanceID), strings.TrimSpace(region))
}

func GetLocalEndpoint() (string, int32, error) {
	host := os.Getenv("LOCAL_HOST")
	if strings.TrimSpace(host) == "" {
		return "", 0, fmt.Errorf("missing LOCAL_HOST environment variable for local database connection")
	}

	portStr := os.Getenv("LOCAL_PORT")
	if strings.TrimSpace(portStr) == "" {
		return "", 0, fmt.Errorf("missing LOCAL_PORT environment variable for local database connection")
	}

	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 {
		return "", 0, fmt.Errorf("invalid LOCAL_PORT %q: must be a valid positive integer port", portStr)
	}

	return host, int32(port), nil
}

func RunPgDump(
	ctx context.Context,
	host string,
	port int32,
	dbname string,
	outputPath string,
	creds Credentials,
	logger *ExecutionLogger,
) error {
	args := []string{
		"--host", host,
		"--port", fmt.Sprintf("%d", port),
		"--username", creds.User,
		"--dbname", dbname,
		"--format=c",
		"--file", outputPath,
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"--verbose",
	}

	if err := runPostgresCommand(ctx, "pg_dump", args, creds, logger); err != nil {
		return fmt.Errorf("create dump for database %q: %w", dbname, err)
	}

	return nil
}

func RunPgRestore(
	ctx context.Context,
	host string,
	port int32,
	dbname string,
	dumpPath string,
	creds Credentials,
	logger *ExecutionLogger,
) error {
	if strings.TrimSpace(dumpPath) == "" {
		return fmt.Errorf("cannot execute pg_restore: dump file path is empty")
	}

	args := []string{
		"--host", host,
		"--port", fmt.Sprintf("%d", port),
		"--username", creds.User,
		"--dbname", dbname,
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"--verbose",
		dumpPath,
	}

	if err := runPostgresCommand(ctx, "pg_restore", args, creds, logger); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			var captured string
			if logger != nil {
				captured = logger.GetLog()
			}
			if IsIgnorableRestoreWarning(captured) {
				var out io.Writer = os.Stdout
				if logger != nil {
					out = logger.GetWriter()
				}
				notice := "\nNote: pg_restore completed with non-fatal extension/role warnings (e.g. pg_stat_kcache). Core data restored successfully.\n"
				fmt.Fprint(out, notice)
				if logger != nil {
					logger.Append([]byte(notice))
				}
				return ErrRestoreWithWarnings
			}
		}
		return fmt.Errorf("restore dump %q into database %q: %w", dumpPath, dbname, err)
	}

	return nil
}

type logWriter struct {
	logger *ExecutionLogger
}

func (l logWriter) Write(p []byte) (n int, err error) {
	if l.logger != nil {
		l.logger.Append(p)
	}
	return len(p), nil
}

func runPostgresCommand(
	ctx context.Context,
	command string,
	args []string,
	credentials Credentials,
	logger *ExecutionLogger,
) error {
	var out io.Writer = os.Stdout
	if logger != nil {
		out = logger.GetWriter()
	}
	cmdStr := fmt.Sprintf("\n$ %s %s\n", command, strings.Join(args, " "))
	fmt.Fprint(out, cmdStr)
	if logger != nil {
		logger.Append([]byte(cmdStr))
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(),
		"PGPASSWORD="+credentials.Password,
		"PGSSLMODE="+credentials.SSLMode,
	)

	lw := logWriter{logger: logger}
	cmd.Stdout = io.MultiWriter(out, lw)
	cmd.Stderr = io.MultiWriter(out, lw)

	if err := cmd.Run(); err != nil {
		errStr := fmt.Sprintf("\nCommand failed with error: %v\n", err)
		if logger != nil {
			logger.Append([]byte(errStr))
		}
		return fmt.Errorf("run %s: %w", command, err)
	}

	return nil
}
