package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
)

const (
	RestoreModeDumpFile = "dump_file"
	RestoreModeBackup   = "backup"
	RestoreModePIT      = "pit"
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

	fs.StringVar(&opts.Action, "action", "", "Operation action: 'dump' (export), 'restore' (import), or 'sync' (copy)")
	fs.StringVar(&opts.Instance, "instance", "", "Source PostgreSQL instance ID/Name or 'local'")
	fs.StringVar(&opts.Instance, "source-instance", "", "Source PostgreSQL instance ID/Name or 'local' (alias)")
	fs.StringVar(&opts.Database, "database", "", "Source database name")
	fs.StringVar(&opts.Database, "source-database", "", "Source database name (alias)")
	fs.StringVar(&opts.TargetInstance, "target-instance", "", "Destination PostgreSQL instance ID/Name or 'local'")
	fs.StringVar(&opts.TargetInstance, "dest-instance", "", "Destination PostgreSQL instance ID/Name or 'local' (alias)")
	fs.StringVar(&opts.TargetDatabase, "target-database", "", "Destination database name")
	fs.StringVar(&opts.TargetDatabase, "dest-database", "", "Destination database name (alias)")
	fs.StringVar(&opts.Mode, "mode", "", "Dump/Sync mode ('live', 'replica', 'pit') or Restore mode ('dump_file', 'backup', 'pit')")
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


	if opts.Action != "" || opts.Instance != "" || opts.Database != "" || opts.TargetInstance != "" || opts.TargetDatabase != "" || opts.NonInteractive {
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
	switch actionLower {
	case "dump", "export":
		o.Action = string(actionDump)
	case "restore", "import":
		o.Action = string(actionRestore)
	case "sync", "copy":
		o.Action = string(actionSync)
	default:
		return fmt.Errorf("invalid or missing --action: must be 'dump' (export), 'restore' (import), or 'sync' (copy)")
	}

	modeLower := strings.ToLower(strings.TrimSpace(o.Mode))

	switch action(o.Action) {
	case actionDump:
		if strings.TrimSpace(o.Instance) == "" && strings.TrimSpace(o.TargetInstance) != "" {
			o.Instance = o.TargetInstance
		}
		if strings.TrimSpace(o.Database) == "" && strings.TrimSpace(o.TargetDatabase) != "" {
			o.Database = o.TargetDatabase
		}

		if strings.TrimSpace(o.Instance) == "" {
			return fmt.Errorf("--instance is required for dump action")
		}
		if strings.TrimSpace(o.Database) == "" {
			return fmt.Errorf("--database is required for dump action")
		}

		if !postgres.HasCredentials(o.Instance) {
			hint := postgres.GetMissingCredentialsHint(o.Instance)
			return fmt.Errorf("instance %q is unavailable: missing %s in environment", o.Instance, hint)
		}

		switch modeLower {
		case "", "live", "standard", string(api.DumpModeStandard):
			o.Mode = string(api.DumpModeStandard)
		case "replica", string(api.DumpModeReplica):
			o.Mode = string(api.DumpModeReplica)
			if strings.EqualFold(o.Instance, "local") {
				return fmt.Errorf("dump mode 'replica' is not supported for local database instance: only 'live' dump is supported on local")
			}
		case "pit", string(api.DumpModePointInTime):
			o.Mode = string(api.DumpModePointInTime)
			if strings.EqualFold(o.Instance, "local") {
				return fmt.Errorf("dump mode 'pit' is not supported for local database instance: only 'live' dump is supported on local")
			}
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
		if strings.TrimSpace(o.TargetInstance) == "" && strings.TrimSpace(o.Instance) != "" {
			o.TargetInstance = o.Instance
		}
		if strings.TrimSpace(o.TargetDatabase) == "" && strings.TrimSpace(o.Database) != "" {
			o.TargetDatabase = o.Database
		}

		if strings.TrimSpace(o.TargetInstance) == "" {
			return fmt.Errorf("--target-instance is required for restore action")
		}
		if strings.TrimSpace(o.TargetDatabase) == "" {
			return fmt.Errorf("--target-database is required for restore action")
		}

		if !postgres.HasCredentials(o.TargetInstance) {
			hint := postgres.GetMissingCredentialsHint(o.TargetInstance)
			return fmt.Errorf("destination instance %q is unavailable: missing %s in environment", o.TargetInstance, hint)
		}

		if modeLower == "" {
			if strings.TrimSpace(o.DumpFile) != "" {
				modeLower = "dump_file"
			} else if strings.TrimSpace(o.Backup) != "" {
				modeLower = "backup"
			} else if strings.TrimSpace(o.PITRaw) != "" {
				modeLower = "pit"
			} else {
				modeLower = "dump_file"
			}
		}

		switch modeLower {
		case "dump_file", "dump":
			o.Mode = RestoreModeDumpFile
			if strings.TrimSpace(o.DumpFile) == "" {
				return fmt.Errorf("--dump-file path is required when restore mode is 'dump_file'")
			}
		case "backup", "stackit_backup":
			o.Mode = RestoreModeBackup
			if strings.TrimSpace(o.Instance) == "" {
				o.Instance = o.TargetInstance
			}
			if strings.EqualFold(o.Instance, "local") {
				return fmt.Errorf("restore from backup is not supported for 'local' instance: backups are only available for STACKIT cloud instances")
			}
			if strings.TrimSpace(o.Backup) == "" {
				return fmt.Errorf("--backup name is required when restore mode is 'backup'")
			}
		case "pit":
			o.Mode = RestoreModePIT
			if strings.TrimSpace(o.Instance) == "" {
				o.Instance = o.TargetInstance
			}
			if strings.EqualFold(o.Instance, "local") {
				return fmt.Errorf("point-in-time restore is not supported for 'local' instance: PIT is only available for STACKIT cloud instances")
			}
			if strings.TrimSpace(o.PITRaw) == "" {
				return fmt.Errorf("--pit datetime string is required when restore mode is 'pit'")
			}
			t, err := ParsePITTimestamp(o.PITRaw)
			if err != nil {
				return fmt.Errorf("invalid --pit value: %w", err)
			}
			o.PITParsed = &t
		default:
			return fmt.Errorf("invalid restore mode %q: must be 'dump_file', 'backup', or 'pit'", o.Mode)
		}

	case actionSync:
		if strings.TrimSpace(o.Instance) == "" {
			return fmt.Errorf("--source-instance is required for sync action")
		}
		if strings.TrimSpace(o.Database) == "" {
			return fmt.Errorf("--source-database is required for sync action")
		}
		if strings.TrimSpace(o.TargetInstance) == "" {
			return fmt.Errorf("--target-instance is required for sync action")
		}
		if strings.TrimSpace(o.TargetDatabase) == "" {
			return fmt.Errorf("--target-database is required for sync action")
		}

		if !postgres.HasCredentials(o.Instance) {
			hint := postgres.GetMissingCredentialsHint(o.Instance)
			return fmt.Errorf("source instance %q is unavailable: missing %s in environment", o.Instance, hint)
		}
		if !postgres.HasCredentials(o.TargetInstance) {
			hint := postgres.GetMissingCredentialsHint(o.TargetInstance)
			return fmt.Errorf("destination instance %q is unavailable: missing %s in environment", o.TargetInstance, hint)
		}

		switch modeLower {
		case "", "live", "standard", string(api.DumpModeStandard):
			o.Mode = string(api.DumpModeStandard)
		case "replica", string(api.DumpModeReplica):
			o.Mode = string(api.DumpModeReplica)
			if strings.EqualFold(o.Instance, "local") {
				return fmt.Errorf("sync mode 'replica' is not supported when source instance is 'local': only 'live' sync is supported on local")
			}
		case "pit", string(api.DumpModePointInTime):
			o.Mode = string(api.DumpModePointInTime)
			if strings.EqualFold(o.Instance, "local") {
				return fmt.Errorf("sync mode 'pit' is not supported when source instance is 'local': only 'live' sync is supported on local")
			}
			if strings.TrimSpace(o.PITRaw) == "" {
				return fmt.Errorf("--pit datetime string is required when sync mode is 'pit'")
			}
			t, err := ParsePITTimestamp(o.PITRaw)
			if err != nil {
				return fmt.Errorf("invalid --pit value: %w", err)
			}
			o.PITParsed = &t
		default:
			return fmt.Errorf("invalid sync mode %q: must be 'live', 'replica', or 'pit'", o.Mode)
		}
	}

	return nil
}

func PrintUsage(w io.Writer) {
	helpText := `PostgreSQL Restore CLI for STACKIT (stackit-restore)

Usage:
  stackit-restore [flags]                   # Guided interactive TUI mode (arrow keys)
  stackit-restore --action=<action> [flags] # Single-line non-interactive mode

Core Actions:
  dump    (alias: export) - Export a PostgreSQL database to a local custom binary .dump file
  restore (alias: import) - Restore a .dump file, STACKIT cloud backup, or PIT snapshot into a target database
  sync    (alias: copy)   - Direct database-to-database replication (dump source -> restore to target)

Global Flags:
  -h, --help                 Show this help screen and usage examples
  --action string            Operation to perform: 'dump', 'restore', or 'sync'
  --instance string          Source PostgreSQL instance ID/Name or 'local' (alias: --source-instance)
  --database string          Source database name (alias: --source-database)
  --target-instance string   Destination PostgreSQL instance ID/Name or 'local' (alias: --dest-instance)
  --target-database string   Destination database name (alias: --dest-database)
  --mode string              Mode: 'live', 'replica', 'pit' (for dump/sync) or 'dump_file', 'backup', 'pit' (for restore)
  --pit string               Point-in-time datetime string (e.g. '2026-08-13 15:00:00' or RFC3339)
  --backup string            Backup name for restore from STACKIT backup mode
  --dump-file string         Path to local .dump file for restore from dump mode
  --sa-key-path string       Path to STACKIT Service Account Key JSON file (alias: --service-account-key-path)
  --project-id string        STACKIT Project ID override
  --region string            STACKIT Region override
  --non-interactive          Force single-line non-interactive mode

Environment Variables:
  STACKIT_PROJECT_ID                  STACKIT Project ID (required for STACKIT operations)
  STACKIT_REGION                      STACKIT Region (required for STACKIT operations)
  STACKIT_SERVICE_ACCOUNT_KEY_PATH    Path to STACKIT Service Account Key JSON file (KeyAuth)
  [INSTANCE_NAME]_USER                PostgreSQL username for specific instance (e.g. PRODUCTION_USER)
  [INSTANCE_NAME]_PASS                PostgreSQL password for specific instance (e.g. PRODUCTION_PASS)
  LOCAL_HOST                          Local PostgreSQL host (e.g. 'localhost' or '127.0.0.1')
  LOCAL_PORT                          Local PostgreSQL port (e.g. '5432')
  LOCAL_DB                            Local PostgreSQL database name (default: 'postgres', alias: LOCAL_DATABASE)
  LOCAL_USER                          Local PostgreSQL username
  LOCAL_PASS                          Local PostgreSQL password
  POSTGRES_DUMP_DIR                   Local directory to store dump artifacts (default: 'dumps')

Single-Line Usage Examples:
  # 1. DUMP / EXPORT:
  # Export live STACKIT database to .dump file:
  stackit-restore --action=dump --instance=Production --database=app_prod --mode=live

  # Export local database to .dump file:
  stackit-restore --action=dump --instance=local --database=app_local --mode=live

  # Export from STACKIT replica at Point-In-Time:
  stackit-restore --action=dump --instance=Production --database=app_prod --mode=pit --pit="2026-08-13 15:00:00"

  # 2. RESTORE / IMPORT:
  # Restore a local .dump file into local database:
  stackit-restore --action=restore --target-instance=local --target-database=app_local --mode=dump_file --dump-file=dumps/custom.dump

  # Restore a STACKIT cloud backup into local database:
  stackit-restore --action=restore --instance=Production --backup=prod-auto-20260112 --target-instance=local --target-database=app_local --mode=backup

  # Restore a STACKIT PIT snapshot into Staging database:
  stackit-restore --action=restore --instance=Production --target-instance=Staging --target-database=app_stg --mode=pit --pit="2026-08-13 15:00:00"

  # 3. SYNC / COPY:
  # Direct DB-to-DB live sync (Production -> local):
  stackit-restore --action=sync --instance=Production --database=app_prod --target-instance=local --target-database=app_local --mode=live

  # Direct DB-to-DB sync from backup replica (Production -> Staging):
  stackit-restore --action=sync --instance=Production --database=app_prod --target-instance=Staging --target-database=app_stg --mode=replica
`
	fmt.Fprint(w, helpText)
}
