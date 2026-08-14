package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func ReadCredentials() (Credentials, error) {
	user := firstNonEmpty(
		os.Getenv("STACKIT_POSTGRES_USER"),
		os.Getenv("POSTGRES_USER"),
		os.Getenv("PGUSER"),
		readSecretFromFile(os.Getenv("STACKIT_POSTGRES_USER_FILE")),
		readSecretFromFile(os.Getenv("POSTGRES_USER_FILE")),
	)
	if strings.TrimSpace(user) == "" {
		return Credentials{}, fmt.Errorf(
			"missing postgres user, set STACKIT_POSTGRES_USER, POSTGRES_USER, PGUSER or STACKIT_POSTGRES_USER_FILE",
		)
	}

	password := firstNonEmpty(
		os.Getenv("STACKIT_POSTGRES_PASSWORD"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("PGPASSWORD"),
		readSecretFromFile(os.Getenv("STACKIT_POSTGRES_PASSWORD_FILE")),
		readSecretFromFile(os.Getenv("POSTGRES_PASSWORD_FILE")),
		readSecretFromFile(os.Getenv("PGPASSFILE")),
	)
	if strings.TrimSpace(password) == "" {
		return Credentials{}, fmt.Errorf(
			"missing postgres password, set STACKIT_POSTGRES_PASSWORD, POSTGRES_PASSWORD, PGPASSWORD or STACKIT_POSTGRES_PASSWORD_FILE",
		)
	}

	sslMode := firstNonEmpty(
		os.Getenv("STACKIT_POSTGRES_SSLMODE"),
		os.Getenv("PGSSLMODE"),
		"require",
	)

	return Credentials{
		User:     user,
		Password: password,
		SSLMode:  sslMode,
	}, nil
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
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(),
		"PGPASSWORD="+credentials.Password,
		"PGSSLMODE="+credentials.SSLMode,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"run %s: %w: %s",
			command,
			err,
			strings.TrimSpace(string(output)),
		)
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func readSecretFromFile(filePath string) string {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return ""
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
