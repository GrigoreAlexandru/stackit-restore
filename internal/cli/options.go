package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
)

type Options struct {
	Action                string
	Instance              string
	Database              string
	TargetInstance        string
	TargetDatabase        string
	Mode                  string
	PITRaw                string
	Backup                string
	DumpFile              string
	ProjectID             string
	Region                string
	ServiceAccountKeyPath string
	NonInteractive        bool
	Help                  bool

	PITParsed *time.Time
}

func ParseOptions(args []string) (Options, error) {
	fs := flag.NewFlagSet("stackit-restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts Options

	fs.StringVar(&opts.Action, "action", "", "Operation action: 'dump' or 'restore'")
	fs.StringVar(&opts.Instance, "instance", "", "Source STACKIT PostgreSQL instance ID or Name")
	fs.StringVar(&opts.Instance, "source-instance", "", "Source STACKIT PostgreSQL instance ID or Name (alias)")
	fs.StringVar(&opts.Database, "database", "", "Source database name")
	fs.StringVar(&opts.Database, "source-database", "", "Source database name (alias)")
	fs.StringVar(&opts.TargetInstance, "target-instance", "", "Destination STACKIT PostgreSQL instance ID or Name")
	fs.StringVar(&opts.TargetInstance, "dest-instance", "", "Destination STACKIT PostgreSQL instance ID or Name (alias)")
	fs.StringVar(&opts.TargetDatabase, "target-database", "", "Destination database name")
	fs.StringVar(&opts.TargetDatabase, "dest-database", "", "Destination database name (alias)")
	fs.StringVar(&opts.Mode, "mode", "", "Dump mode ('live', 'replica', 'pit') or Restore mode ('live_db', 'stackit_backup', 'pit', 'dump_file')")
	fs.StringVar(&opts.PITRaw, "pit", "", "Point-in-time datetime string (e.g. '2026-08-13 15:00:00' or RFC3339)")
	fs.StringVar(&opts.Backup, "backup", "", "Backup name for restore from Stackit backup mode")
	fs.StringVar(&opts.DumpFile, "dump-file", "", "Path to local .dump file for restore from dump mode")
	fs.StringVar(&opts.ProjectID, "project-id", "", "STACKIT Project ID override")
	fs.StringVar(&opts.Region, "region", "", "STACKIT Region override")
	fs.StringVar(&opts.ServiceAccountKeyPath, "sa-key-path", "", "Path to STACKIT Service Account Key JSON file")
	fs.StringVar(&opts.ServiceAccountKeyPath, "service-account-key-path", "", "Path to STACKIT Service Account Key JSON file (alias)")
	fs.BoolVar(&opts.NonInteractive, "non-interactive", false, "Run in non-interactive mode without TUI prompts")
	fs.BoolVar(&opts.Help, "help", false, "Show help message and usage examples")
	fs.BoolVar(&opts.Help, "h", false, "Show help message and usage examples (shorthand)")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}

	if opts.Help {
		return opts, nil
	}

	if opts.ProjectID != "" {
		os.Setenv("STACKIT_PROJECT_ID", opts.ProjectID)
	}
	if opts.Region != "" {
		os.Setenv("STACKIT_REGION", opts.Region)
	}
	if opts.ServiceAccountKeyPath != "" {
		os.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", opts.ServiceAccountKeyPath)
	}

	if opts.Action != "" || opts.Instance != "" || opts.Database != "" || opts.NonInteractive {
		opts.NonInteractive = true
		if err := opts.Validate(); err != nil {
			return Options{}, err
		}
	}

	return opts, nil
}

func (o *Options) Validate() error {
	if !o.NonInteractive {
		return nil
	}

	actionLower := strings.ToLower(strings.TrimSpace(o.Action))
	if actionLower != string(actionDump) && actionLower != string(actionRestore) {
		return fmt.Errorf("invalid or missing --action: must be 'dump' or 'restore'")
	}
	o.Action = actionLower

	if strings.TrimSpace(o.Instance) == "" {
		return fmt.Errorf("--instance is required in single-line / non-interactive mode")
	}

	if strings.TrimSpace(o.Database) == "" {
		return fmt.Errorf("--database is required in single-line / non-interactive mode")
	}

	if strings.TrimSpace(o.TargetInstance) == "" {
		o.TargetInstance = o.Instance
	}

	if strings.TrimSpace(o.TargetDatabase) == "" {
		o.TargetDatabase = o.Database
	}

	modeLower := strings.ToLower(strings.TrimSpace(o.Mode))

	switch action(o.Action) {
	case actionDump:
		switch modeLower {
		case "", "live", "standard", string(api.DumpModeStandard):
			o.Mode = string(api.DumpModeStandard)
		case "replica", string(api.DumpModeReplica):
			o.Mode = string(api.DumpModeReplica)
		case "pit", string(api.DumpModePointInTime):
			o.Mode = string(api.DumpModePointInTime)
			if strings.TrimSpace(o.PITRaw) == "" {
				return fmt.Errorf("--pit datetime string is required when dump mode is 'pit'")
			}
			t, err := ParsePITTimestamp(o.PITRaw)
			if err != nil {
				return fmt.Errorf("invalid --pit value: %w", err)
			}
			o.PITParsed = &t
		default:
			return fmt.Errorf("invalid dump mode %q: must be 'live', 'replica', or 'pit'", o.Mode)
		}

	case actionRestore:
		switch modeLower {
		case "", "live", "live_db", string(restoreFromLiveDB):
			o.Mode = string(restoreFromLiveDB)
		case "dump_file", "dump", string(restoreFromDump):
			o.Mode = string(restoreFromDump)
			if strings.TrimSpace(o.DumpFile) == "" {
				return fmt.Errorf("--dump-file path is required when restore mode is 'dump_file'")
			}
		case "backup", "stackit_backup", string(restoreFromStackitBackup):
			o.Mode = string(restoreFromStackitBackup)
			if strings.TrimSpace(o.Backup) == "" {
				return fmt.Errorf("--backup name is required when restore mode is 'stackit_backup'")
			}
		case "pit", string(restoreFromPIT):
			o.Mode = string(restoreFromPIT)
			if strings.TrimSpace(o.PITRaw) == "" {
				return fmt.Errorf("--pit datetime string is required when restore mode is 'pit'")
			}
			t, err := ParsePITTimestamp(o.PITRaw)
			if err != nil {
				return fmt.Errorf("invalid --pit value: %w", err)
			}
			o.PITParsed = &t
		default:
			return fmt.Errorf("invalid restore mode %q: must be 'live_db', 'stackit_backup', 'pit', or 'dump_file'", o.Mode)
		}
	}

	return nil
}

func PrintUsage(w io.Writer) {
	helpText := `PostgreSQL Restore CLI for STACKIT (stackit-restore)

Usage:
  stackit-restore [flags]                   # Guided interactive TUI mode (arrow keys)
  stackit-restore --action=<action> [flags] # Single-line non-interactive mode

Global Flags:
  -h, --help                 Show this help screen and usage examples
  --action string            Operation to perform: 'dump' or 'restore'
  --instance string          Source STACKIT PostgreSQL instance ID or Name (alias: --source-instance)
  --database string          Source database name (alias: --source-database)
  --target-instance string   Destination STACKIT PostgreSQL instance ID or Name (alias: --dest-instance, defaults to --instance)
  --target-database string   Destination database name (alias: --dest-database, defaults to --database)
  --mode string              Dump mode ('live', 'replica', 'pit') or Restore mode ('live_db', 'stackit_backup', 'pit', 'dump_file')
  --pit string               Point-in-time datetime string (e.g. '2026-08-13 15:00:00' or RFC3339)
  --backup string            Backup name for restore from Stackit backup mode
  --dump-file string         Path to local .dump file for restore from dump mode
  --sa-key-path string       Path to STACKIT Service Account Key JSON file (alias: --service-account-key-path)
  --project-id string        STACKIT Project ID override
  --region string            STACKIT Region override
  --non-interactive          Force single-line non-interactive mode

Environment Variables:
  STACKIT_PROJECT_ID                  STACKIT Project ID (required)
  STACKIT_REGION                      STACKIT Region (required)
  STACKIT_SERVICE_ACCOUNT_KEY_PATH    Path to STACKIT Service Account Key JSON file (KeyAuth flow)
  STACKIT_SERVICE_ACCOUNT_TOKEN       STACKIT Service Account Bearer Token (TokenAuth flow)
  STACKIT_POSTGRES_USER               PostgreSQL username (or STACKIT_POSTGRES_USER_FILE)
  STACKIT_POSTGRES_PASSWORD           PostgreSQL password (or STACKIT_POSTGRES_PASSWORD_FILE)
  POSTGRES_DUMP_DIR                   Local directory to store dump artifacts (default: 'dumps')

Single-Line Usage Examples:
  # Dump from live data directly using KeyAuth flow:
  stackit-restore --action=dump --instance=Production --database=app_prod --mode=live --sa-key-path=/etc/stackit/key.json

  # Dump from a STACKIT replica at a specific Point-In-Time datetime:
  stackit-restore --action=dump --instance=Production --database=app_prod --mode=pit --pit="2026-08-13 15:00:00"

  # Restore directly from a live source database into a destination database:
  stackit-restore --action=restore --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=live_db

  # Restore a STACKIT backup into a destination database:
  stackit-restore --action=restore --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=stackit_backup --backup=prod-auto-20260112

  # Restore from a STACKIT replica at Point-In-Time datetime:
  stackit-restore --action=restore --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=pit --pit="2026-08-13 15:00:00"

  # Restore a database from a local .dump file:
  stackit-restore --action=restore --instance=Staging --database=app_stg --mode=dump_file --dump-file=/tmp/dumps/dump.dump
`
	fmt.Fprint(w, helpText)
}
