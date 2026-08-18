package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
)

var (
	unavailableStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dateHighlightStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
)

type API interface {
	GetInstances(ctx context.Context) ([]api.Instance, error)
	GetDatabases(ctx context.Context, instance api.Instance) ([]api.Database, error)
	GetBackups(ctx context.Context, instance api.Instance) ([]api.Backup, error)
	ListDumpArtifacts(ctx context.Context) ([]api.DumpArtifact, error)
	CreateDump(ctx context.Context, instance api.Instance, database api.Database, mode api.DumpMode, pit *time.Time) (api.DumpArtifact, error)
	RestoreDump(ctx context.Context, instance api.Instance, database api.Database, dump api.DumpArtifact) error
	RestoreFromPIT(ctx context.Context, instance api.Instance, database api.Database, pit time.Time) (api.DumpArtifact, error)
}

type action string

const (
	actionDump    action = "dump"
	actionRestore action = "restore"
	actionSync    action = "sync"
)

type restoreSourceType string

const (
	restoreSourceDumpFile    restoreSourceType = "dump_file"
	restoreSourceCloudBackup restoreSourceType = "cloud_backup"
	restoreSourceCloudPIT    restoreSourceType = "cloud_pit"
)

type databaseSelection struct {
	Instance api.Instance
	Database api.Database
}

type appForm struct {
	selectedAction action

	// Dump & Sync Source
	sourceSelection  databaseSelection
	selectedDumpMode api.DumpMode

	// Restore & Sync Target
	destSelection databaseSelection

	// Restore Source Details
	selectedRestoreSource restoreSourceType
	selectedCloudInstance api.Instance
	selectedBackup        api.Backup
	selectedPIT           time.Time
	selectedDump          api.DumpArtifact

	databaseSelections []databaseSelection
	backupsByInstance  map[string][]api.Backup
	dumpArtifacts      []api.DumpArtifact
	instances          []api.Instance

	apiClient API
}

func (a *appForm) buildExplanation() string {
	switch a.selectedAction {
	case actionDump:
		switch a.selectedDumpMode {
		case api.DumpModeStandard:
			return fmt.Sprintf(
				"Runs pg_dump directly on live database %q of instance %q and saves the output as a custom binary .dump file.",
				a.sourceSelection.Database.Name,
				a.sourceSelection.Instance.Name,
			)
		case api.DumpModeReplica:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone instance in STACKIT from latest backup of %q, runs pg_dump on database %q to generate a .dump file, and automatically deletes the temporary clone.",
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
			)
		case api.DumpModePointInTime:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone instance in STACKIT from point-in-time %s of instance %q, runs pg_dump on database %q to generate a .dump file, and automatically deletes the temporary clone.",
				a.selectedPIT.Format(time.RFC3339),
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
			)
		}

	case actionRestore:
		switch a.selectedRestoreSource {
		case restoreSourceDumpFile:
			return fmt.Sprintf(
				"Reads local .dump file %q and restores it into target database %q of instance %q using pg_restore (--clean --if-exists --no-owner --no-privileges --verbose).\n\nFlags Explanation:\n- --clean --if-exists: Drops existing database objects before recreating them to prevent duplicate table/key errors.\n- --no-owner: Prevents setting object ownership to match the original database, avoiding permission errors across different database users.\n- --no-privileges: Skips restoring access privileges (GRANT/REVOKE), preserving the target database's existing security configurations.",
				a.selectedDump.Path,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
			)
		case restoreSourceCloudBackup:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone in STACKIT from backup %q (%s) of %q, extracts dump, deletes clone, and restores into target database %q of instance %q using pg_restore (--clean --if-exists --no-owner --no-privileges --verbose).\n\nFlags Explanation:\n- --clean --if-exists: Drops existing database objects before recreating them.\n- --no-owner: Prevents setting object ownership to match the original database.\n- --no-privileges: Skips restoring access privileges (GRANT/REVOKE), preserving target security settings.",
				a.selectedBackup.Name,
				a.selectedBackup.CreatedAt.Format(time.RFC3339),
				a.selectedCloudInstance.Name,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
			)
		case restoreSourceCloudPIT:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone in STACKIT from point-in-time %s of %q, extracts dump, deletes clone, and restores into target database %q of instance %q using pg_restore (--clean --if-exists --no-owner --no-privileges --verbose).\n\nFlags Explanation:\n- --clean --if-exists: Drops existing database objects before recreating them.\n- --no-owner: Prevents setting object ownership to match the original database.\n- --no-privileges: Skips restoring access privileges (GRANT/REVOKE), preserving target security settings.",
				a.selectedPIT.Format(time.RFC3339),
				a.selectedCloudInstance.Name,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
			)
		}

	case actionSync:
		switch a.selectedDumpMode {
		case api.DumpModeStandard:
			return fmt.Sprintf(
				"Extracts live dump from %s / %s and restores it directly into %s / %s using pg_restore (--clean --if-exists --no-owner --no-privileges --verbose).\n\nFlags Explanation:\n- --clean --if-exists: Drops existing database objects before recreating them.\n- --no-owner: Prevents setting object ownership to match the source database, avoiding user mismatch errors.\n- --no-privileges: Skips restoring access privileges (GRANT/REVOKE), keeping target permissions intact.",
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
				a.destSelection.Instance.Name,
				a.destSelection.Database.Name,
			)
		case api.DumpModeReplica:
			return fmt.Sprintf(
				"Creates temporary STACKIT clone from latest backup of %s, extracts dump of %s, deletes clone, and restores into %s / %s using pg_restore (--clean --if-exists --no-owner --no-privileges --verbose).\n\nFlags Explanation:\n- --clean --if-exists: Drops existing database objects before recreating them.\n- --no-owner: Prevents setting object ownership to match the source database.\n- --no-privileges: Skips restoring access privileges (GRANT/REVOKE).",
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
				a.destSelection.Instance.Name,
				a.destSelection.Database.Name,
			)
		case api.DumpModePointInTime:
			return fmt.Sprintf(
				"Creates temporary STACKIT clone from point-in-time %s of %s, extracts dump of %s, deletes clone, and restores into %s / %s using pg_restore (--clean --if-exists --no-owner --no-privileges --verbose).\n\nFlags Explanation:\n- --clean --if-exists: Drops existing database objects before recreating them.\n- --no-owner: Prevents setting object ownership to match the source database.\n- --no-privileges: Skips restoring access privileges (GRANT/REVOKE).",
				a.selectedPIT.Format(time.RFC3339),
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
				a.destSelection.Instance.Name,
				a.destSelection.Database.Name,
			)
		}
	}
	return ""
}

func ExecuteWithOptions(ctx context.Context, apiClient API, opts Options) error {
	if opts.Help {
		PrintUsage(os.Stdout)
		return nil
	}

	if opts.NonInteractive {
		return runNonInteractive(ctx, apiClient, opts)
	}

	return Execute(ctx, apiClient)
}

func getDumpDir() string {
	dir := os.Getenv("POSTGRES_DUMP_DIR")
	if strings.TrimSpace(dir) == "" {
		return "dumps"
	}
	return dir
}

func handleExecutionError(actionName string, contextDetails map[string]string, tracker *StepTracker, failedStep int, err error) error {
	if tracker != nil {
		tracker.FailStep(failedStep, err)
		tracker.RenderSummary()
	}

	logPath, logErr := postgres.WriteErrorLog(getDumpDir(), actionName, contextDetails, err)

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("ERROR: Operation failed.")
	if logErr == nil && logPath != "" {
		fmt.Printf("A detailed error log has been written to: %s\n", logPath)
	}
	fmt.Println(strings.Repeat("=", 80))

	return err
}

func runNonInteractive(ctx context.Context, apiClient API, opts Options) error {
	instances, err := apiClient.GetInstances(ctx)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	findInstance := func(nameOrID string) (api.Instance, error) {
		for _, inst := range instances {
			if strings.EqualFold(inst.ID, nameOrID) || strings.EqualFold(inst.Name, nameOrID) {
				return inst, nil
			}
		}
		return api.Instance{}, fmt.Errorf("instance %q not found", nameOrID)
	}

	findDatabase := func(inst api.Instance, dbName string) (api.Database, error) {
		dbs, err := apiClient.GetDatabases(ctx, inst)
		if err != nil {
			return api.Database{}, fmt.Errorf("get databases for instance %q: %w", inst.Name, err)
		}
		for _, db := range dbs {
			if strings.EqualFold(db.Name, dbName) {
				return db, nil
			}
		}
		if strings.EqualFold(inst.Name, "local") {
			return api.Database{Name: dbName, ID: 1, Owner: "postgres"}, nil
		}
		return api.Database{}, fmt.Errorf("database %q not found in instance %q", dbName, inst.Name)
	}

	postgres.ResetExecutionBuffer()

	switch action(opts.Action) {
	case actionDump:
		srcInst, err := findInstance(opts.Instance)
		if err != nil {
			return err
		}
		srcDB, err := findDatabase(srcInst, opts.Database)
		if err != nil {
			return err
		}

		if !postgres.HasCredentials(srcInst.Name) {
			hint := postgres.GetMissingCredentialsHint(srcInst.Name)
			return fmt.Errorf("instance %q is unavailable: missing %s in environment", srcInst.Name, hint)
		}

		mode := api.DumpMode(opts.Mode)
		if strings.EqualFold(srcInst.Name, "local") && mode != api.DumpModeStandard {
			return fmt.Errorf("dump mode %q is not supported for local database instance: only 'live' dump is supported on local", mode)
		}

		var steps []string
		if mode == api.DumpModeStandard {
			steps = []string{
				fmt.Sprintf("Extract live database dump from %s / %s", srcInst.Name, srcDB.Name),
			}
		} else if mode == api.DumpModeReplica {
			steps = []string{
				fmt.Sprintf("Provision temporary clone from latest backup of %s in STACKIT", srcInst.Name),
				fmt.Sprintf("Extract dump from temporary clone (%s)", srcDB.Name),
				"Delete temporary STACKIT clone instance",
			}
		} else {
			pitStr := "PIT"
			if opts.PITParsed != nil {
				pitStr = opts.PITParsed.Format(time.RFC3339)
			}
			steps = []string{
				fmt.Sprintf("Provision temporary clone at %s in STACKIT", pitStr),
				fmt.Sprintf("Extract dump from temporary clone (%s)", srcDB.Name),
				"Delete temporary STACKIT clone instance",
			}
		}

		contextDetails := map[string]string{
			"Instance": srcInst.Name,
			"Database": srcDB.Name,
			"Mode":     string(mode),
		}
		if opts.PITParsed != nil {
			contextDetails["PIT"] = opts.PITParsed.Format(time.RFC3339)
		}

		var artifact api.DumpArtifact
		err = RunWithStepView(
			ctx,
			"PostgreSQL Dump Execution",
			steps,
			"dump",
			contextDetails,
			func(execCtx context.Context, reporter StepReporter) error {
				reporter.StartStep(0)
				dump, err := apiClient.CreateDump(execCtx, srcInst, srcDB, mode, opts.PITParsed)
				deleteForbidden := false
				if err != nil {
					if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
						deleteForbidden = true
					} else {
						reporter.FailStep(0, err)
						return err
					}
				}
				if mode == api.DumpModeStandard {
					reporter.CompleteStep(0)
				} else {
					reporter.CompleteStep(0)
					reporter.CompleteStep(1)
					if deleteForbidden {
						reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
					} else {
						reporter.CompleteStep(2)
					}
				}
				artifact = dump
				return nil
			},
		)
		if err != nil {
			return err
		}

		fmt.Printf("Dump created successfully: %s\n", artifact.Path)
		return nil

	case actionRestore:
		dstInst, err := findInstance(opts.TargetInstance)
		if err != nil {
			return err
		}
		dstDB, err := findDatabase(dstInst, opts.TargetDatabase)
		if err != nil {
			return err
		}

		if !postgres.HasCredentials(dstInst.Name) {
			hint := postgres.GetMissingCredentialsHint(dstInst.Name)
			return fmt.Errorf("destination instance %q is unavailable: missing %s in environment", dstInst.Name, hint)
		}

		contextDetails := map[string]string{
			"DestinationInstance": dstInst.Name,
			"DestinationDatabase": dstDB.Name,
			"Mode":                opts.Mode,
		}

		switch opts.Mode {
		case "dump_file":
			contextDetails["DumpFile"] = opts.DumpFile
			steps := []string{
				fmt.Sprintf("Restore dump file %s into %s / %s", filepath.Base(opts.DumpFile), dstInst.Name, dstDB.Name),
			}
			return RunWithStepView(
				ctx,
				"PostgreSQL Restore Execution",
				steps,
				"restore",
				contextDetails,
				func(execCtx context.Context, reporter StepReporter) error {
					reporter.StartStep(0)
					dumpArtifact := api.DumpArtifact{
						Name:         filepath.Base(opts.DumpFile),
						Path:         opts.DumpFile,
						Mode:         api.DumpModeStandard,
						InstanceName: dstInst.Name,
						InstanceID:   dstInst.ID,
						DatabaseName: dstDB.Name,
						CreatedAt:    time.Now().UTC(),
					}
					if err := apiClient.RestoreDump(execCtx, dstInst, dstDB, dumpArtifact); err != nil {
						if errors.Is(err, postgres.ErrRestoreWithWarnings) {
							reporter.CompleteStepWithWarning(0, "non-critical extension/role warnings ignored")
							fmt.Printf("Restore completed with warnings from file: %s\n", opts.DumpFile)
							return nil
						}
						reporter.FailStep(0, err)
						return err
					}
					reporter.CompleteStep(0)
					fmt.Printf("Restore completed successfully from file: %s\n", opts.DumpFile)
					return nil
				},
			)

		case "backup", "stackit_backup":
			srcInst, err := findInstance(opts.Instance)
			if err != nil {
				return err
			}
			if strings.EqualFold(srcInst.Name, "local") {
				return fmt.Errorf("restore from backup is not supported for 'local' instance: backups are only available for STACKIT cloud instances")
			}

			backups, err := apiClient.GetBackups(ctx, srcInst)
			if err != nil {
				return fmt.Errorf("get backups for instance %q: %w", srcInst.Name, err)
			}
			var targetBackup *api.Backup
			for _, b := range backups {
				if strings.EqualFold(b.Name, opts.Backup) {
					targetBackup = &b
					break
				}
			}
			if targetBackup == nil {
				return fmt.Errorf("backup %q not found for instance %q", opts.Backup, srcInst.Name)
			}

			contextDetails["SourceInstance"] = srcInst.Name
			contextDetails["Backup"] = targetBackup.Name

			steps := []string{
				fmt.Sprintf("Provision temporary clone from backup %s in STACKIT", targetBackup.Name),
				fmt.Sprintf("Extract dump from temporary clone (%s)", dstDB.Name),
				"Delete temporary STACKIT clone instance",
				fmt.Sprintf("Restore dump into target database %s / %s", dstInst.Name, dstDB.Name),
			}

			var artifact api.DumpArtifact
			err = RunWithStepView(
				ctx,
				"PostgreSQL Restore Execution",
				steps,
				"restore",
				contextDetails,
				func(execCtx context.Context, reporter StepReporter) error {
					reporter.StartStep(0)
					dump, err := apiClient.CreateDump(execCtx, srcInst, dstDB, api.DumpModeReplica, &targetBackup.CreatedAt)
					deleteForbidden := false
					if err != nil {
						if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
							deleteForbidden = true
						} else {
							reporter.FailStep(0, err)
							return err
						}
					}
					reporter.CompleteStep(0)
					reporter.CompleteStep(1)
					if deleteForbidden {
						reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
					} else {
						reporter.CompleteStep(2)
					}

					reporter.StartStep(3)
					if err := apiClient.RestoreDump(execCtx, dstInst, dstDB, dump); err != nil {
						if errors.Is(err, postgres.ErrRestoreWithWarnings) {
							reporter.CompleteStepWithWarning(3, "non-critical extension/role warnings ignored")
							artifact = dump
							return nil
						}
						reporter.FailStep(3, err)
						return err
					}
					reporter.CompleteStep(3)
					artifact = dump
					return nil
				},
			)
			if err != nil {
				return err
			}
			fmt.Printf("Restore from backup completed successfully using dump: %s\n", artifact.Path)
			return nil

		case "pit":
			srcInst, err := findInstance(opts.Instance)
			if err != nil {
				return err
			}
			if strings.EqualFold(srcInst.Name, "local") {
				return fmt.Errorf("point-in-time restore is not supported for 'local' instance: PIT is only available for STACKIT cloud instances")
			}
			if opts.PITParsed == nil {
				return fmt.Errorf("missing parsed PIT timestamp")
			}

			contextDetails["SourceInstance"] = srcInst.Name
			contextDetails["PIT"] = opts.PITParsed.Format(time.RFC3339)

			steps := []string{
				fmt.Sprintf("Provision temporary clone at %s in STACKIT", opts.PITParsed.Format(time.RFC3339)),
				fmt.Sprintf("Extract dump from temporary clone (%s)", dstDB.Name),
				"Delete temporary STACKIT clone instance",
				fmt.Sprintf("Restore dump into target database %s / %s", dstInst.Name, dstDB.Name),
			}

			var artifact api.DumpArtifact
			err = RunWithStepView(
				ctx,
				"PostgreSQL Restore Execution",
				steps,
				"restore",
				contextDetails,
				func(execCtx context.Context, reporter StepReporter) error {
					reporter.StartStep(0)
					dump, err := apiClient.CreateDump(execCtx, srcInst, dstDB, api.DumpModePointInTime, opts.PITParsed)
					deleteForbidden := false
					if err != nil {
						if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
							deleteForbidden = true
						} else {
							reporter.FailStep(0, err)
							return err
						}
					}
					reporter.CompleteStep(0)
					reporter.CompleteStep(1)
					if deleteForbidden {
						reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
					} else {
						reporter.CompleteStep(2)
					}

					reporter.StartStep(3)
					if err := apiClient.RestoreDump(execCtx, dstInst, dstDB, dump); err != nil {
						if errors.Is(err, postgres.ErrRestoreWithWarnings) {
							reporter.CompleteStepWithWarning(3, "non-critical extension/role warnings ignored")
							artifact = dump
							return nil
						}
						reporter.FailStep(3, err)
						return err
					}
					reporter.CompleteStep(3)
					artifact = dump
					return nil
				},
			)
			if err != nil {
				return err
			}
			fmt.Printf("Restore from PIT completed successfully using dump: %s\n", artifact.Path)
			return nil

		default:
			return fmt.Errorf("unsupported restore mode %q", opts.Mode)
		}

	case actionSync:
		srcInst, err := findInstance(opts.Instance)
		if err != nil {
			return err
		}
		srcDB, err := findDatabase(srcInst, opts.Database)
		if err != nil {
			return err
		}

		dstInst, err := findInstance(opts.TargetInstance)
		if err != nil {
			return err
		}
		dstDB, err := findDatabase(dstInst, opts.TargetDatabase)
		if err != nil {
			return err
		}

		if !postgres.HasCredentials(srcInst.Name) {
			hint := postgres.GetMissingCredentialsHint(srcInst.Name)
			return fmt.Errorf("source instance %q is unavailable: missing %s in environment", srcInst.Name, hint)
		}
		if !postgres.HasCredentials(dstInst.Name) {
			hint := postgres.GetMissingCredentialsHint(dstInst.Name)
			return fmt.Errorf("destination instance %q is unavailable: missing %s in environment", dstInst.Name, hint)
		}

		mode := api.DumpMode(opts.Mode)
		if strings.EqualFold(srcInst.Name, "local") && mode != api.DumpModeStandard {
			return fmt.Errorf("sync mode %q is not supported when source instance is 'local': only 'live' sync is supported on local", mode)
		}

		var steps []string
		if mode == api.DumpModeStandard {
			steps = []string{
				fmt.Sprintf("Extract live dump from source database %s / %s", srcInst.Name, srcDB.Name),
				fmt.Sprintf("Restore dump into target database %s / %s", dstInst.Name, dstDB.Name),
			}
		} else if mode == api.DumpModeReplica {
			steps = []string{
				fmt.Sprintf("Provision temporary clone from latest backup of %s in STACKIT", srcInst.Name),
				fmt.Sprintf("Extract dump from temporary clone (%s)", srcDB.Name),
				"Delete temporary STACKIT clone instance",
				fmt.Sprintf("Restore dump into target database %s / %s", dstInst.Name, dstDB.Name),
			}
		} else {
			pitStr := "PIT"
			if opts.PITParsed != nil {
				pitStr = opts.PITParsed.Format(time.RFC3339)
			}
			steps = []string{
				fmt.Sprintf("Provision temporary clone at %s in STACKIT", pitStr),
				fmt.Sprintf("Extract dump from temporary clone (%s)", srcDB.Name),
				"Delete temporary STACKIT clone instance",
				fmt.Sprintf("Restore dump into target database %s / %s", dstInst.Name, dstDB.Name),
			}
		}

		contextDetails := map[string]string{
			"SourceInstance": srcInst.Name,
			"SourceDatabase": srcDB.Name,
			"TargetInstance": dstInst.Name,
			"TargetDatabase": dstDB.Name,
			"Mode":           string(mode),
		}
		if opts.PITParsed != nil {
			contextDetails["PIT"] = opts.PITParsed.Format(time.RFC3339)
		}

		var artifact api.DumpArtifact
		err = RunWithStepView(
			ctx,
			"PostgreSQL Database Sync Execution",
			steps,
			"sync",
			contextDetails,
			func(execCtx context.Context, reporter StepReporter) error {
				reporter.StartStep(0)
				dump, err := apiClient.CreateDump(execCtx, srcInst, srcDB, mode, opts.PITParsed)
				deleteForbidden := false
				if err != nil {
					if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
						deleteForbidden = true
					} else {
						reporter.FailStep(0, err)
						return err
					}
				}

				if mode == api.DumpModeStandard {
					reporter.CompleteStep(0)
				} else {
					reporter.CompleteStep(0)
					reporter.CompleteStep(1)
					if deleteForbidden {
						reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
					} else {
						reporter.CompleteStep(2)
					}
				}

				restoreStepIdx := len(steps) - 1
				reporter.StartStep(restoreStepIdx)
				if err := apiClient.RestoreDump(execCtx, dstInst, dstDB, dump); err != nil {
					if errors.Is(err, postgres.ErrRestoreWithWarnings) {
						reporter.CompleteStepWithWarning(restoreStepIdx, "non-critical extension/role warnings ignored")
						artifact = dump
						return nil
					}
					reporter.FailStep(restoreStepIdx, err)
					return err
				}
				reporter.CompleteStep(restoreStepIdx)
				artifact = dump
				return nil
			},
		)
		if err != nil {
			return err
		}

		fmt.Printf("Sync completed successfully from %s / %s into %s / %s using dump: %s\n", srcInst.Name, srcDB.Name, dstInst.Name, dstDB.Name, artifact.Path)
		return nil

	default:
		return fmt.Errorf("unsupported action %q", opts.Action)
	}
}

func Execute(ctx context.Context, apiClient API) error {
	app := &appForm{
		apiClient: apiClient,
	}

	err := spinner.New().
		Context(ctx).
		ActionWithErr(app.preloadResources).
		Accessible(false).
		Title("Preloading instances, databases and backups...").
		Run()
	if err != nil {
		return err
	}

	actionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[action]().
				Options(
					huh.NewOption("Dump to File (Export a database to a custom binary .dump file)", actionDump),
					huh.NewOption("Restore to Database (Apply a .dump file, cloud backup, or PIT snapshot into a target database)", actionRestore),
					huh.NewOption("Sync Databases (Copy data directly from one database to another)", actionSync),
				).
				Value(&app.selectedAction).
				Title("What would you like to do?"),
		),
	)

	if err := actionForm.Run(); err != nil {
		return err
	}

	switch app.selectedAction {
	case actionDump:
		return app.runDumpFlow(ctx)
	case actionRestore:
		return app.runRestoreFlow(ctx)
	case actionSync:
		return app.runSyncFlow(ctx)
	}

	return nil
}

func (a *appForm) runDumpFlow(ctx context.Context) error {
	sourceForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[databaseSelection]().
				OptionsFunc(a.getDatabaseOptions, a.databaseSelections).
				Validate(func(s databaseSelection) error {
					if !postgres.HasCredentials(s.Instance.Name) {
						hint := postgres.GetMissingCredentialsHint(s.Instance.Name)
						return fmt.Errorf("instance %q is unavailable: missing %s", s.Instance.Name, hint)
					}
					return nil
				}).
				Value(&a.sourceSelection).
				Title("Please select the database to export").
				Height(5),
		),
	)
	if err := sourceForm.Run(); err != nil {
		return err
	}

	if strings.EqualFold(a.sourceSelection.Instance.Name, "local") {
		a.selectedDumpMode = api.DumpModeStandard
	} else {
		dumpModeForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[api.DumpMode]().
					Options(
						huh.NewOption("Live Database (Direct pg_dump on live instance)", api.DumpModeStandard),
						huh.NewOption("STACKIT Replica (Zero-impact clone from latest backup)", api.DumpModeReplica),
						huh.NewOption("STACKIT Replica (Point-In-Time snapshot clone)", api.DumpModePointInTime),
					).
					Value(&a.selectedDumpMode).
					Title("Please select dump extraction strategy").
					Description(fmt.Sprintf("Target Database: %s / %s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name)),
			),
		)
		if err := dumpModeForm.Run(); err != nil {
			return err
		}

		if a.selectedDumpMode == api.DumpModeReplica {
			if err := a.selectBackupForInstance(a.sourceSelection.Instance); err != nil {
				return err
			}
			a.selectedPIT = a.selectedBackup.CreatedAt
		} else if a.selectedDumpMode == api.DumpModePointInTime {
			if err := a.promptPITTimestamp(a.sourceSelection.Instance); err != nil {
				return err
			}
		}
	}

	var confirm bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Confirm Dump Operation?").
				Description(fmt.Sprintf("Target Database: %s / %s\n\nExplanation:\n%s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name, a.buildExplanation())).
				Value(&confirm),
		),
	)
	if err := confirmForm.Run(); err != nil {
		return err
	}
	if !confirm {
		fmt.Println("Operation cancelled by user.")
		return nil
	}

	var steps []string
	if a.selectedDumpMode == api.DumpModeStandard {
		steps = []string{
			fmt.Sprintf("Extract live database dump from %s / %s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name),
		}
	} else if a.selectedDumpMode == api.DumpModeReplica {
		backupName := a.selectedBackup.Name
		if backupName == "" {
			backupName = "selected backup"
		}
		steps = []string{
			fmt.Sprintf("Provision temporary clone from backup %s (%s) in STACKIT", backupName, a.sourceSelection.Instance.Name),
			fmt.Sprintf("Extract dump from temporary clone (%s)", a.sourceSelection.Database.Name),
			"Delete temporary STACKIT clone instance",
		}
	} else {
		steps = []string{
			fmt.Sprintf("Provision temporary clone at %s in STACKIT", a.selectedPIT.Format(time.RFC3339)),
			fmt.Sprintf("Extract dump from temporary clone (%s)", a.sourceSelection.Database.Name),
			"Delete temporary STACKIT clone instance",
		}
	}

	contextDetails := map[string]string{
		"Instance": a.sourceSelection.Instance.Name,
		"Database": a.sourceSelection.Database.Name,
		"Mode":     string(a.selectedDumpMode),
	}
	if a.selectedDumpMode == api.DumpModePointInTime {
		contextDetails["PIT"] = a.selectedPIT.Format(time.RFC3339)
	} else if a.selectedDumpMode == api.DumpModeReplica && a.selectedBackup.Name != "" {
		contextDetails["Backup"] = a.selectedBackup.Name
	}

	var artifact api.DumpArtifact
	err := RunWithStepView(
		ctx,
		"PostgreSQL Dump Execution",
		steps,
		"dump",
		contextDetails,
		func(execCtx context.Context, reporter StepReporter) error {
			var pit *time.Time
			if a.selectedDumpMode == api.DumpModePointInTime || a.selectedDumpMode == api.DumpModeReplica {
				pit = &a.selectedPIT
			}

			reporter.StartStep(0)
			dump, err := a.apiClient.CreateDump(
				execCtx,
				a.sourceSelection.Instance,
				a.sourceSelection.Database,
				a.selectedDumpMode,
				pit,
			)
			deleteForbidden := false
			if err != nil {
				if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
					deleteForbidden = true
				} else {
					reporter.FailStep(0, err)
					return err
				}
			}

			if a.selectedDumpMode == api.DumpModeStandard {
				reporter.CompleteStep(0)
			} else {
				reporter.CompleteStep(0)
				reporter.CompleteStep(1)
				if deleteForbidden {
					reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
				} else {
					reporter.CompleteStep(2)
				}
			}
			artifact = dump
			return nil
		},
	)
	if err != nil {
		return err
	}

	fmt.Printf("Dump created successfully for %s / %s: %s\n", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name, artifact.Path)
	return nil
}

func (a *appForm) runRestoreFlow(ctx context.Context) error {
	destForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[databaseSelection]().
				OptionsFunc(a.getDestinationDatabaseOptions, a.databaseSelections).
				Validate(func(s databaseSelection) error {
					if !postgres.HasCredentials(s.Instance.Name) {
						hint := postgres.GetMissingCredentialsHint(s.Instance.Name)
						return fmt.Errorf("instance %q is unavailable: missing %s", s.Instance.Name, hint)
					}
					return nil
				}).
				Value(&a.destSelection).
				Title("Please select the target database to restore into").
				Height(5),
		),
	)
	if err := destForm.Run(); err != nil {
		return err
	}

	sourceTypeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[restoreSourceType]().
				Options(
					huh.NewOption("Local .dump File (Select from dumps/ directory)", restoreSourceDumpFile),
					huh.NewOption("STACKIT Cloud Backup (Restore from automated cloud backup)", restoreSourceCloudBackup),
					huh.NewOption("STACKIT Point-In-Time Snapshot (Restore from PIT timestamp)", restoreSourceCloudPIT),
				).
				Value(&a.selectedRestoreSource).
				Title("Where would you like to restore data from?").
				Description(fmt.Sprintf("Target Database: %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name)),
		),
	)
	if err := sourceTypeForm.Run(); err != nil {
		return err
	}

	switch a.selectedRestoreSource {
	case restoreSourceDumpFile:
		if err := a.loadDumpArtifacts(ctx); err != nil {
			return err
		}
		if len(a.dumpArtifacts) == 0 {
			return fmt.Errorf("no dumps found in dump directory")
		}

		dumpSelectForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[api.DumpArtifact]().
					OptionsFunc(a.getDumpOptions, a.dumpArtifacts).
					Value(&a.selectedDump).
					Title("Please select the .dump file to restore").
					Description(fmt.Sprintf("Target Database: %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name)).
					Height(5),
			),
		)
		if err := dumpSelectForm.Run(); err != nil {
			return err
		}

	case restoreSourceCloudBackup:
		if err := a.selectCloudInstance(); err != nil {
			return err
		}
		if err := a.selectBackupForInstance(a.selectedCloudInstance); err != nil {
			return err
		}

	case restoreSourceCloudPIT:
		if err := a.selectCloudInstance(); err != nil {
			return err
		}
		if err := a.promptPITTimestamp(a.selectedCloudInstance); err != nil {
			return err
		}
	}

	var confirm bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Confirm Restore Operation? (Target database will be overwritten)").
				Description(fmt.Sprintf("Target Database: %s / %s\n\nExplanation:\n%s", a.destSelection.Instance.Name, a.destSelection.Database.Name, a.buildExplanation())).
				Value(&confirm),
		),
	)
	if err := confirmForm.Run(); err != nil {
		return err
	}
	if !confirm {
		fmt.Println("Operation cancelled by user.")
		return nil
	}

	var steps []string
	switch a.selectedRestoreSource {
	case restoreSourceDumpFile:
		steps = []string{
			fmt.Sprintf("Restore dump file %s into %s / %s", filepath.Base(a.selectedDump.Path), a.destSelection.Instance.Name, a.destSelection.Database.Name),
		}
	case restoreSourceCloudBackup:
		steps = []string{
			fmt.Sprintf("Provision temporary clone from backup %s in STACKIT", a.selectedBackup.Name),
			fmt.Sprintf("Extract dump from temporary clone (%s)", a.destSelection.Database.Name),
			"Delete temporary STACKIT clone instance",
			fmt.Sprintf("Restore dump into target database %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name),
		}
	case restoreSourceCloudPIT:
		steps = []string{
			fmt.Sprintf("Provision temporary clone at %s in STACKIT", a.selectedPIT.Format(time.RFC3339)),
			fmt.Sprintf("Extract dump from temporary clone (%s)", a.destSelection.Database.Name),
			"Delete temporary STACKIT clone instance",
			fmt.Sprintf("Restore dump into target database %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name),
		}
	}

	contextDetails := map[string]string{
		"DestinationInstance": a.destSelection.Instance.Name,
		"DestinationDatabase": a.destSelection.Database.Name,
		"SourceType":          string(a.selectedRestoreSource),
	}

	var generatedDump api.DumpArtifact
	err := RunWithStepView(
		ctx,
		"PostgreSQL Restore Execution",
		steps,
		"restore",
		contextDetails,
		func(execCtx context.Context, reporter StepReporter) error {
			switch a.selectedRestoreSource {
			case restoreSourceDumpFile:
				contextDetails["DumpFile"] = a.selectedDump.Path
				reporter.StartStep(0)
				if err := a.apiClient.RestoreDump(execCtx, a.destSelection.Instance, a.destSelection.Database, a.selectedDump); err != nil {
					if errors.Is(err, postgres.ErrRestoreWithWarnings) {
						reporter.CompleteStepWithWarning(0, "non-critical extension/role warnings ignored")
						generatedDump = a.selectedDump
						return nil
					}
					reporter.FailStep(0, err)
					return err
				}
				reporter.CompleteStep(0)
				generatedDump = a.selectedDump
				return nil

			case restoreSourceCloudBackup, restoreSourceCloudPIT:
				pit := a.selectedPIT
				if a.selectedRestoreSource == restoreSourceCloudBackup {
					contextDetails["CloudInstance"] = a.selectedCloudInstance.Name
					contextDetails["Backup"] = a.selectedBackup.Name
					pit = a.selectedBackup.CreatedAt
				} else {
					contextDetails["CloudInstance"] = a.selectedCloudInstance.Name
					contextDetails["PIT"] = a.selectedPIT.Format(time.RFC3339)
				}

				reporter.StartStep(0)
				dump, err := a.apiClient.CreateDump(
					execCtx,
					a.selectedCloudInstance,
					a.destSelection.Database,
					api.DumpModePointInTime,
					&pit,
				)
				deleteForbidden := false
				if err != nil {
					if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
						deleteForbidden = true
					} else {
						reporter.FailStep(0, err)
						return err
					}
				}

				reporter.CompleteStep(0)
				reporter.CompleteStep(1)
				if deleteForbidden {
					reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
				} else {
					reporter.CompleteStep(2)
				}

				reporter.StartStep(3)
				if err := a.apiClient.RestoreDump(execCtx, a.destSelection.Instance, a.destSelection.Database, dump); err != nil {
					if errors.Is(err, postgres.ErrRestoreWithWarnings) {
						reporter.CompleteStepWithWarning(3, "non-critical extension/role warnings ignored")
						generatedDump = dump
						return nil
					}
					reporter.FailStep(3, err)
					return err
				}
				reporter.CompleteStep(3)
				generatedDump = dump
				return nil
			}
			return nil
		},
	)
	if err != nil {
		return err
	}

	fmt.Printf("Restore completed successfully into %s / %s using dump: %s\n", a.destSelection.Instance.Name, a.destSelection.Database.Name, generatedDump.Path)
	return nil
}

func (a *appForm) runSyncFlow(ctx context.Context) error {
	sourceForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[databaseSelection]().
				OptionsFunc(a.getDatabaseOptions, a.databaseSelections).
				Validate(func(s databaseSelection) error {
					if !postgres.HasCredentials(s.Instance.Name) {
						hint := postgres.GetMissingCredentialsHint(s.Instance.Name)
						return fmt.Errorf("instance %q is unavailable: missing %s", s.Instance.Name, hint)
					}
					return nil
				}).
				Value(&a.sourceSelection).
				Title("Please select the source database to copy from").
				Height(5),
		),
	)
	if err := sourceForm.Run(); err != nil {
		return err
	}

	destForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[databaseSelection]().
				OptionsFunc(a.getDestinationDatabaseOptions, a.databaseSelections).
				Validate(func(s databaseSelection) error {
					if !postgres.HasCredentials(s.Instance.Name) {
						hint := postgres.GetMissingCredentialsHint(s.Instance.Name)
						return fmt.Errorf("instance %q is unavailable: missing %s", s.Instance.Name, hint)
					}
					return nil
				}).
				Value(&a.destSelection).
				Title("Please select the target database to copy into").
				Description(fmt.Sprintf("Source Database: %s / %s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name)).
				Height(5),
		),
	)
	if err := destForm.Run(); err != nil {
		return err
	}

	if strings.EqualFold(a.sourceSelection.Instance.Name, "local") {
		a.selectedDumpMode = api.DumpModeStandard
	} else {
		syncModeForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[api.DumpMode]().
					Options(
						huh.NewOption("Live Sync (Direct dump from live source -> restore into target)", api.DumpModeStandard),
						huh.NewOption("Backup-Based Sync (Clone from source backup -> restore -> cleanup)", api.DumpModeReplica),
						huh.NewOption("Point-In-Time Sync (Clone from source PIT -> restore -> cleanup)", api.DumpModePointInTime),
					).
					Value(&a.selectedDumpMode).
					Title("Please select extraction strategy for source database").
					Description(fmt.Sprintf("Source: %s / %s  ->  Destination: %s / %s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name, a.destSelection.Instance.Name, a.destSelection.Database.Name)),
			),
		)
		if err := syncModeForm.Run(); err != nil {
			return err
		}

		if a.selectedDumpMode == api.DumpModeReplica {
			if err := a.selectBackupForInstance(a.sourceSelection.Instance); err != nil {
				return err
			}
			a.selectedPIT = a.selectedBackup.CreatedAt
		} else if a.selectedDumpMode == api.DumpModePointInTime {
			if err := a.promptPITTimestamp(a.sourceSelection.Instance); err != nil {
				return err
			}
		}
	}

	var confirm bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Confirm Database Sync Operation? (Target database will be overwritten)").
				Description(fmt.Sprintf("Source: %s / %s\nDestination: %s / %s\n\nExplanation:\n%s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name, a.destSelection.Instance.Name, a.destSelection.Database.Name, a.buildExplanation())).
				Value(&confirm),
		),
	)
	if err := confirmForm.Run(); err != nil {
		return err
	}
	if !confirm {
		fmt.Println("Operation cancelled by user.")
		return nil
	}

	var steps []string
	if a.selectedDumpMode == api.DumpModeStandard {
		steps = []string{
			fmt.Sprintf("Extract live dump from source database %s / %s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name),
			fmt.Sprintf("Restore dump into target database %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name),
		}
	} else if a.selectedDumpMode == api.DumpModeReplica {
		backupName := a.selectedBackup.Name
		if backupName == "" {
			backupName = "selected backup"
		}
		steps = []string{
			fmt.Sprintf("Provision temporary clone from backup %s (%s) in STACKIT", backupName, a.sourceSelection.Instance.Name),
			fmt.Sprintf("Extract dump from temporary clone (%s)", a.sourceSelection.Database.Name),
			"Delete temporary STACKIT clone instance",
			fmt.Sprintf("Restore dump into target database %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name),
		}
	} else {
		pitStr := "PIT"
		if a.selectedDumpMode == api.DumpModePointInTime {
			pitStr = a.selectedPIT.Format(time.RFC3339)
		}
		steps = []string{
			fmt.Sprintf("Provision temporary clone at %s in STACKIT", pitStr),
			fmt.Sprintf("Extract dump from temporary clone (%s)", a.sourceSelection.Database.Name),
			"Delete temporary STACKIT clone instance",
			fmt.Sprintf("Restore dump into target database %s / %s", a.destSelection.Instance.Name, a.destSelection.Database.Name),
		}
	}

	contextDetails := map[string]string{
		"SourceInstance": a.sourceSelection.Instance.Name,
		"SourceDatabase": a.sourceSelection.Database.Name,
		"TargetInstance": a.destSelection.Instance.Name,
		"TargetDatabase": a.destSelection.Database.Name,
		"Mode":           string(a.selectedDumpMode),
	}
	if a.selectedDumpMode == api.DumpModePointInTime {
		contextDetails["PIT"] = a.selectedPIT.Format(time.RFC3339)
	} else if a.selectedDumpMode == api.DumpModeReplica && a.selectedBackup.Name != "" {
		contextDetails["Backup"] = a.selectedBackup.Name
	}

	var artifact api.DumpArtifact
	err := RunWithStepView(
		ctx,
		"PostgreSQL Database Sync Execution",
		steps,
		"sync",
		contextDetails,
		func(execCtx context.Context, reporter StepReporter) error {
			var pit *time.Time
			if a.selectedDumpMode == api.DumpModePointInTime || a.selectedDumpMode == api.DumpModeReplica {
				pit = &a.selectedPIT
			}

			reporter.StartStep(0)
			dump, err := a.apiClient.CreateDump(
				execCtx,
				a.sourceSelection.Instance,
				a.sourceSelection.Database,
				a.selectedDumpMode,
				pit,
			)
			deleteForbidden := false
			if err != nil {
				if errors.Is(err, stackit.ErrDeleteInstanceForbidden) || stackit.IsDeleteForbidden(err) {
					deleteForbidden = true
				} else {
					reporter.FailStep(0, err)
					return err
				}
			}

			if a.selectedDumpMode == api.DumpModeStandard {
				reporter.CompleteStep(0)
			} else {
				reporter.CompleteStep(0)
				reporter.CompleteStep(1)
				if deleteForbidden {
					reporter.CompleteStepWithWarning(2, "instance could not be deleted due to permissions")
				} else {
					reporter.CompleteStep(2)
				}
			}

			restoreStepIdx := len(steps) - 1
			reporter.StartStep(restoreStepIdx)
			if err := a.apiClient.RestoreDump(
				execCtx,
				a.destSelection.Instance,
				a.destSelection.Database,
				dump,
			); err != nil {
				if errors.Is(err, postgres.ErrRestoreWithWarnings) {
					reporter.CompleteStepWithWarning(restoreStepIdx, "non-critical extension/role warnings ignored")
					artifact = dump
					return nil
				}
				reporter.FailStep(restoreStepIdx, err)
				return err
			}
			reporter.CompleteStep(restoreStepIdx)
			artifact = dump
			return nil
		},
	)
	if err != nil {
		return err
	}

	fmt.Printf(
		"Sync completed successfully from %s / %s into %s / %s using dump: %s\n",
		a.sourceSelection.Instance.Name,
		a.sourceSelection.Database.Name,
		a.destSelection.Instance.Name,
		a.destSelection.Database.Name,
		artifact.Path,
	)
	return nil
}

func (a *appForm) preloadResources(ctx context.Context) error {
	instances, err := a.apiClient.GetInstances(ctx)
	if err != nil {
		return err
	}

	if len(instances) == 0 {
		return fmt.Errorf("no instances found")
	}

	a.instances = instances
	a.backupsByInstance = make(map[string][]api.Backup, len(instances))
	a.databaseSelections = make([]databaseSelection, 0, len(instances))

	for _, instance := range instances {
		databases, err := a.apiClient.GetDatabases(ctx, instance)
		if err != nil {
			return fmt.Errorf("get databases for instance %q: %w", instance.Name, err)
		}

		backups, err := a.apiClient.GetBackups(ctx, instance)
		if err != nil {
			return fmt.Errorf("get backups for instance %q: %w", instance.Name, err)
		}

		a.backupsByInstance[instance.ID] = backups

		for _, database := range databases {
			a.databaseSelections = append(a.databaseSelections, databaseSelection{
				Instance: instance,
				Database: database,
			})
		}
	}

	if len(a.databaseSelections) == 0 {
		return fmt.Errorf("no databases found in any instance")
	}

	return nil
}

func (a *appForm) getDatabaseOptions() []huh.Option[databaseSelection] {
	var available []databaseSelection
	var unavailable []databaseSelection

	for _, selection := range a.databaseSelections {
		if postgres.HasCredentials(selection.Instance.Name) {
			available = append(available, selection)
		} else {
			unavailable = append(unavailable, selection)
		}
	}

	sort.Slice(available, func(i, j int) bool {
		if available[i].Instance.Name == available[j].Instance.Name {
			return available[i].Database.Name < available[j].Database.Name
		}
		return available[i].Instance.Name < available[j].Instance.Name
	})

	sort.Slice(unavailable, func(i, j int) bool {
		if unavailable[i].Instance.Name == unavailable[j].Instance.Name {
			return unavailable[i].Database.Name < unavailable[j].Database.Name
		}
		return unavailable[i].Instance.Name < unavailable[j].Instance.Name
	})

	var options []huh.Option[databaseSelection]
	for _, selection := range available {
		label := fmt.Sprintf("%s / %s", selection.Instance.Name, selection.Database.Name)
		options = append(options, huh.NewOption(label, selection))
	}
	for _, selection := range unavailable {
		hint := postgres.GetMissingCredentialsHint(selection.Instance.Name)
		rawLabel := fmt.Sprintf("%s / %s (unavailable: missing %s)", selection.Instance.Name, selection.Database.Name, hint)
		options = append(options, huh.NewOption(unavailableStyle.Render(rawLabel), selection))
	}

	return options
}

func (a *appForm) getDestinationDatabaseOptions() []huh.Option[databaseSelection] {
	return a.getDatabaseOptions()
}

func (a *appForm) getCloudInstanceOptions() []huh.Option[api.Instance] {
	var available []api.Instance
	var unavailable []api.Instance

	for _, inst := range a.instances {
		if strings.EqualFold(inst.Name, "local") || strings.EqualFold(inst.ID, "local") {
			continue
		}
		if postgres.HasCredentials(inst.Name) {
			available = append(available, inst)
		} else {
			unavailable = append(unavailable, inst)
		}
	}

	sort.Slice(available, func(i, j int) bool {
		return available[i].Name < available[j].Name
	})
	sort.Slice(unavailable, func(i, j int) bool {
		return unavailable[i].Name < unavailable[j].Name
	})

	var options []huh.Option[api.Instance]
	for _, inst := range available {
		options = append(options, huh.NewOption(inst.Name, inst))
	}
	for _, inst := range unavailable {
		hint := postgres.GetMissingCredentialsHint(inst.Name)
		rawLabel := fmt.Sprintf("%s (unavailable: missing %s)", inst.Name, hint)
		options = append(options, huh.NewOption(unavailableStyle.Render(rawLabel), inst))
	}

	return options
}

func (a *appForm) selectCloudInstance() error {
	options := a.getCloudInstanceOptions()
	if len(options) == 0 {
		return fmt.Errorf("no STACKIT cloud instances found for cloud backup restore")
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[api.Instance]().
				Options(options...).
				Validate(func(inst api.Instance) error {
					if !postgres.HasCredentials(inst.Name) {
						hint := postgres.GetMissingCredentialsHint(inst.Name)
						return fmt.Errorf("instance %q is unavailable: missing %s", inst.Name, hint)
					}
					return nil
				}).
				Value(&a.selectedCloudInstance).
				Title("Please select the STACKIT cloud instance providing the backup"),
		),
	)
	return form.Run()
}

func (a *appForm) selectBackupForInstance(inst api.Instance) error {
	backups := a.backupsByInstance[inst.ID]
	if len(backups) == 0 {
		return fmt.Errorf("no backups available for instance %q", inst.Name)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	options := make([]huh.Option[api.Backup], len(backups))
	for i, backup := range backups {
		sizeGB := float64(backup.Size) / (1024 * 1024 * 1024)
		label := fmt.Sprintf("%s | %s | %.2f GB", backup.Name, backup.CreatedAt.Format(time.RFC3339), sizeGB)
		options[i] = huh.NewOption(label, backup)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[api.Backup]().
				Options(options...).
				Value(&a.selectedBackup).
				Title("Please select the cloud backup to restore").
				Height(5),
		),
	)
	return form.Run()
}

func (a *appForm) getDumpOptions() []huh.Option[api.DumpArtifact] {
	options := make([]huh.Option[api.DumpArtifact], len(a.dumpArtifacts))
	for i, dump := range a.dumpArtifacts {
		label := fmt.Sprintf(
			"%s | %s | %s | %s",
			dump.CreatedAt.Format(time.RFC3339),
			dump.Mode,
			dump.InstanceName,
			dump.DatabaseName,
		)
		options[i] = huh.NewOption(label, dump)
	}
	return options
}

func (a *appForm) loadDumpArtifacts(ctx context.Context) error {
	var artifacts []api.DumpArtifact
	err := spinner.New().
		Context(ctx).
		Accessible(false).
		Title("Loading dumps...").
		ActionWithErr(func(ctx context.Context) error {
			dumps, err := a.apiClient.ListDumpArtifacts(ctx)
			if err != nil {
				return err
			}
			artifacts = dumps
			return nil
		}).
		Run()
	if err != nil {
		return err
	}

	a.dumpArtifacts = artifacts
	return nil
}

func (a *appForm) promptPITTimestamp(inst api.Instance) error {
	backups := a.backupsByInstance[inst.ID]

	description := "Format: YYYY-MM-DD HH:MM:SS or RFC3339 (e.g. 2026-08-13 15:00:00)"
	if len(backups) > 0 {
		oldest := backups[0].CreatedAt
		newest := backups[0].CreatedAt
		for _, b := range backups {
			if b.CreatedAt.Before(oldest) {
				oldest = b.CreatedAt
			}
			if b.CreatedAt.After(newest) {
				newest = b.CreatedAt
			}
		}
		oldestStr := dateHighlightStyle.Render(oldest.Format(time.RFC3339))
		newestStr := dateHighlightStyle.Render(newest.Format(time.RFC3339))
		description = fmt.Sprintf(
			"Available backups from: %s to %s\nFormat: YYYY-MM-DD HH:MM:SS or RFC3339 (e.g. 2026-08-13 15:00:00)",
			oldestStr,
			newestStr,
		)
	}

	var rawInput string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Enter Point-In-Time datetime (UTC)").
				Description(description).
				Value(&rawInput).
				Validate(func(val string) error {
					_, err := ParsePITTimestamp(val)
					return err
				}),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	t, err := ParsePITTimestamp(rawInput)
	if err != nil {
		return err
	}
	a.selectedPIT = t
	return nil
}

func ParsePITTimestamp(input string) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return time.Time{}, fmt.Errorf("datetime cannot be empty")
	}

	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, input); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid datetime %q: expected RFC3339 or YYYY-MM-DD HH:MM:SS", input)
}
