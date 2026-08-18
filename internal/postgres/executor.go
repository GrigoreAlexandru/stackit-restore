package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

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

	if err := runPostgresCommand(ctx, "pg_dump", args, creds); err != nil {
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
) error {
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

	if err := runPostgresCommand(ctx, "pg_restore", args, creds); err != nil {
		return fmt.Errorf("restore dump %q into database %q: %w", dumpPath, dbname, err)
	}

	return nil
}

func runPostgresCommand(
	ctx context.Context,
	command string,
	args []string,
	credentials Credentials,
) error {
	fmt.Printf("\n$ %s %s\n", command, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(),
		"PGPASSWORD="+credentials.Password,
		"PGSSLMODE="+credentials.SSLMode,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", command, err)
	}

	return nil
}
