package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ProjectID             string
	Region                string
	ServiceAccountKeyPath string

	LocalHost string
	LocalPort int
	LocalDB   string
	LocalUser string
	LocalPass string

	OperationPollIntervalSeconds int
	OperationTimeoutSeconds      int
	DumpDir                      string
}

const (
	defaultPollInterval = 10
	defaultTimeout      = 600
	defaultDumpDir      = "dumps"
	defaultLocalHost    = "localhost"
	defaultLocalPort    = 5432
	defaultLocalDB      = "postgres"
)

func Default() Config {
	return Config{
		LocalHost:                    defaultLocalHost,
		LocalPort:                    defaultLocalPort,
		LocalDB:                      defaultLocalDB,
		OperationPollIntervalSeconds: defaultPollInterval,
		OperationTimeoutSeconds:      defaultTimeout,
		DumpDir:                      defaultDumpDir,
	}
}

func Load() (Config, error) {
	loadDotEnv(".env")

	cfg := Default()

	cfg.ProjectID = os.Getenv("STACKIT_PROJECT_ID")
	cfg.Region = os.Getenv("STACKIT_REGION")
	cfg.ServiceAccountKeyPath = os.Getenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH")

	if host := os.Getenv("LOCAL_HOST"); host != "" {
		cfg.LocalHost = host
	}
	if portStr := os.Getenv("LOCAL_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.LocalPort = p
		}
	}
	if db := os.Getenv("LOCAL_DB"); db != "" {
		cfg.LocalDB = db
	} else if db := os.Getenv("LOCAL_DATABASE"); db != "" {
		cfg.LocalDB = db
	}
	cfg.LocalUser = os.Getenv("LOCAL_USER")
	cfg.LocalPass = os.Getenv("LOCAL_PASS")

	if value := os.Getenv("STACKIT_OPERATION_POLL_INTERVAL_SECONDS"); value != "" {
		interval, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf(
				"parse STACKIT_OPERATION_POLL_INTERVAL_SECONDS from environment: %w",
				err,
			)
		}
		cfg.OperationPollIntervalSeconds = interval
	}

	if value := os.Getenv("STACKIT_OPERATION_TIMEOUT_SECONDS"); value != "" {
		timeout, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf(
				"parse STACKIT_OPERATION_TIMEOUT_SECONDS from environment: %w",
				err,
			)
		}
		cfg.OperationTimeoutSeconds = timeout
	}

	if value := os.Getenv("POSTGRES_DUMP_DIR"); value != "" {
		cfg.DumpDir = value
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	if err := os.MkdirAll(cfg.DumpDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create dump directory: %w", err)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("STACKIT_PROJECT_ID is required")
	}

	if strings.TrimSpace(c.Region) == "" {
		return fmt.Errorf("STACKIT_REGION is required")
	}

	if strings.TrimSpace(c.ServiceAccountKeyPath) == "" {
		return fmt.Errorf("STACKIT_SERVICE_ACCOUNT_KEY_PATH is required")
	}

	if c.OperationPollIntervalSeconds <= 0 {
		return fmt.Errorf(
			"STACKIT_OPERATION_POLL_INTERVAL_SECONDS must be greater than 0",
		)
	}

	if c.OperationTimeoutSeconds <= 0 {
		return fmt.Errorf(
			"STACKIT_OPERATION_TIMEOUT_SECONDS must be greater than 0",
		)
	}

	if strings.TrimSpace(c.DumpDir) == "" {
		return fmt.Errorf("POSTGRES_DUMP_DIR must not be empty")
	}

	return nil
}

func (c Config) HasAuth() bool {
	return strings.TrimSpace(c.ServiceAccountKeyPath) != ""
}

func loadDotEnv(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}
