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
	ServiceAccountKey     string
	PrivateKeyPath        string
	ServiceAccountToken   string

	OperationPollIntervalSeconds int
	OperationTimeoutSeconds      int
	DumpDir                      string
}

const (
	defaultPollInterval = 10
	defaultTimeout      = 600
	defaultDumpDir      = "dumps"
)

func Default() Config {
	return Config{
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

	cfg.ServiceAccountKeyPath = firstNonEmpty(
		os.Getenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH"),
		os.Getenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH_FILE"),
		os.Getenv("STACKIT_CREDENTIALS_PATH"),
	)
	cfg.ServiceAccountKey = os.Getenv("STACKIT_SERVICE_ACCOUNT_KEY")
	cfg.PrivateKeyPath = firstNonEmpty(
		os.Getenv("STACKIT_PRIVATE_KEY_PATH"),
		os.Getenv("STACKIT_PRIVATE_KEY"),
	)
	cfg.ServiceAccountToken = firstNonEmpty(
		os.Getenv("STACKIT_SERVICE_ACCOUNT_TOKEN"),
		os.Getenv("STACKIT_TOKEN"),
		os.Getenv("STACKIT_SA_TOKEN"),
		readSecretFromFile(os.Getenv("STACKIT_SERVICE_ACCOUNT_TOKEN_FILE")),
	)

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

	if !c.HasAuth() {
		return fmt.Errorf("STACKIT authentication is required: set STACKIT_SERVICE_ACCOUNT_KEY_PATH, STACKIT_SERVICE_ACCOUNT_KEY, or STACKIT_SERVICE_ACCOUNT_TOKEN")
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
	if strings.TrimSpace(c.ServiceAccountKeyPath) != "" ||
		strings.TrimSpace(c.ServiceAccountKey) != "" ||
		strings.TrimSpace(c.PrivateKeyPath) != "" ||
		strings.TrimSpace(c.ServiceAccountToken) != "" {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		credPath := home + "/.stackit/credentials.json"
		if _, err := os.Stat(credPath); err == nil {
			return true
		}
	}
	return false
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
