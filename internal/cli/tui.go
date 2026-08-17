package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
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
)

type databaseSelection struct {
	Instance api.Instance
	Database api.Database
}

type appForm struct {
	sourceSelection     databaseSelection
	destSelection       databaseSelection
	selectedAction      action
	selectedDumpMode    api.DumpMode
	selectedRestoreMode restoreMode
	selectedBackup      api.Backup
	selectedPIT         time.Time
	selectedDump        api.DumpArtifact

	databaseSelections []databaseSelection
	backupsByInstance  map[string][]api.Backup
	dumpArtifacts      []api.DumpArtifact

	apiClient API
}

type restoreMode string

const (
	restoreFromLiveDB        restoreMode = "restore_from_live_db"
	restoreFromStackitBackup restoreMode = "restore_from_stackit_backup"
	restoreFromPIT           restoreMode = "restore_from_pit"
	restoreFromDump          restoreMode = "restore_from_dump"
)

type pitInputMethod string

const (
	pitMethodSelectBackup pitInputMethod = "select_backup"
	pitMethodEnterCustom  pitInputMethod = "enter_custom"
)

func (a *appForm) selectedDBHeader() string {
	if a.sourceSelection.Instance.Name == "" || a.sourceSelection.Database.Name == "" {
		return ""
	}
	if a.selectedAction == actionRestore && a.destSelection.Instance.Name != "" {
		return fmt.Sprintf(
			"Source Database: %s / %s\nDestination Database: %s / %s",
			a.sourceSelection.Instance.Name,
			a.sourceSelection.Database.Name,
			a.destSelection.Instance.Name,
			a.destSelection.Database.Name,
		)
	}
	return fmt.Sprintf("Source Database: %s / %s", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name)
}

func (a *appForm) buildExplanation() string {
	if a.selectedAction == actionDump {
		switch a.selectedDumpMode {
		case api.DumpModeStandard:
			return fmt.Sprintf(
				"Runs pg_dump directly on live source database %q of instance %q and saves the output as a custom binary .dump file.",
				a.sourceSelection.Database.Name,
				a.sourceSelection.Instance.Name,
			)
		case api.DumpModeReplica:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone instance in STACKIT from the latest backup of %q, runs pg_dump on database %q to generate a .dump file, and then deletes the temporary clone instance.",
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
			)
		case api.DumpModePointInTime:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone instance in STACKIT from point-in-time %s of instance %q, runs pg_dump on database %q to generate a .dump file, and then deletes the temporary clone instance.",
				a.selectedPIT.Format(time.RFC3339),
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
			)
		}
	} else if a.selectedAction == actionRestore {
		switch a.selectedRestoreMode {
		case restoreFromLiveDB:
			return fmt.Sprintf(
				"Runs pg_dump directly on live source database %q of instance %q to generate a temporary .dump file, and restores it into destination database %q of instance %q using pg_restore.",
				a.sourceSelection.Database.Name,
				a.sourceSelection.Instance.Name,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
			)
		case restoreFromStackitBackup:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone instance in STACKIT from backup %q (%s) of source instance %q, runs pg_dump on database %q to generate a .dump file, deletes the clone, and restores into destination database %q of instance %q using pg_restore.",
				a.selectedBackup.Name,
				a.selectedBackup.CreatedAt.Format(time.RFC3339),
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
			)
		case restoreFromPIT:
			return fmt.Sprintf(
				"Creates a temporary PostgreSQL clone instance in STACKIT from point-in-time %s of source instance %q, runs pg_dump on database %q to generate a .dump file, deletes the clone, and restores into destination database %q of instance %q using pg_restore.",
				a.selectedPIT.Format(time.RFC3339),
				a.sourceSelection.Instance.Name,
				a.sourceSelection.Database.Name,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
			)
		case restoreFromDump:
			return fmt.Sprintf(
				"Reads local .dump file %q and restores it into destination database %q of instance %q using pg_restore.",
				a.selectedDump.Path,
				a.destSelection.Database.Name,
				a.destSelection.Instance.Name,
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

func runNonInteractive(ctx context.Context, apiClient API, opts Options) error {
	instances, err := apiClient.GetInstances(ctx)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	var sourceInstance api.Instance
	foundSourceInst := false
	for _, inst := range instances {
		if strings.EqualFold(inst.ID, opts.Instance) || strings.EqualFold(inst.Name, opts.Instance) {
			sourceInstance = inst
			foundSourceInst = true
			break
		}
	}
	if !foundSourceInst {
		return fmt.Errorf("source instance %q not found", opts.Instance)
	}

	databases, err := apiClient.GetDatabases(ctx, sourceInstance)
	if err != nil {
		return fmt.Errorf("get databases for source instance %q: %w", sourceInstance.Name, err)
	}

	var sourceDatabase api.Database
	foundSourceDB := false
	for _, db := range databases {
		if strings.EqualFold(db.Name, opts.Database) {
			sourceDatabase = db
			foundSourceDB = true
			break
		}
	}
	if !foundSourceDB {
		return fmt.Errorf("source database %q not found in instance %q", opts.Database, sourceInstance.Name)
	}

	if !postgres.HasCredentials(sourceInstance.Name) {
		hint := postgres.GetMissingCredentialsHint(sourceInstance.Name)
		return fmt.Errorf("source instance %q is unavailable: missing %s in environment", sourceInstance.Name, hint)
	}

	switch action(opts.Action) {
	case actionDump:
		mode := api.DumpMode(opts.Mode)
		fmt.Println("================================================================================")
		fmt.Println("PostgreSQL Operation Summary & Explanation")
		fmt.Println("================================================================================")
		fmt.Printf("Action:   Dump (%s)\n", mode)
		fmt.Printf("Source:   %s / %s\n", sourceInstance.Name, sourceDatabase.Name)
		if opts.PITParsed != nil {
			fmt.Printf("PIT:      %s\n", opts.PITParsed.Format(time.RFC3339))
		}
		fmt.Println("================================================================================")

		artifact, err := apiClient.CreateDump(ctx, sourceInstance, sourceDatabase, mode, opts.PITParsed)
		if err != nil {
			return fmt.Errorf("create dump: %w", err)
		}
		fmt.Printf("Dump created successfully: %s\n", artifact.Path)
		return nil

	case actionRestore:
		var targetInstance api.Instance
		foundTargetInst := false
		for _, inst := range instances {
			if strings.EqualFold(inst.ID, opts.TargetInstance) || strings.EqualFold(inst.Name, opts.TargetInstance) {
				targetInstance = inst
				foundTargetInst = true
				break
			}
		}
		if !foundTargetInst {
			return fmt.Errorf("destination instance %q not found", opts.TargetInstance)
		}

		if !postgres.HasCredentials(targetInstance.Name) {
			hint := postgres.GetMissingCredentialsHint(targetInstance.Name)
			return fmt.Errorf("destination instance %q is unavailable: missing %s in environment", targetInstance.Name, hint)
		}

		targetDatabases, err := apiClient.GetDatabases(ctx, targetInstance)
		if err != nil {
			return fmt.Errorf("get databases for destination instance %q: %w", targetInstance.Name, err)
		}

		var targetDatabase api.Database
		foundTargetDB := false
		for _, db := range targetDatabases {
			if strings.EqualFold(db.Name, opts.TargetDatabase) {
				targetDatabase = db
				foundTargetDB = true
				break
			}
		}
		if !foundTargetDB {
			if strings.EqualFold(targetInstance.Name, "local") {
				targetDatabase = api.Database{Name: opts.TargetDatabase, ID: 1, Owner: "postgres"}
			} else {
				return fmt.Errorf("destination database %q not found in instance %q", opts.TargetDatabase, targetInstance.Name)
			}
		}

		mode := restoreMode(opts.Mode)

		fmt.Println("================================================================================")
		fmt.Println("PostgreSQL Operation Summary & Explanation")
		fmt.Println("================================================================================")
		fmt.Printf("Action:      Restore (%s)\n", mode)
		fmt.Printf("Source:      %s / %s\n", sourceInstance.Name, sourceDatabase.Name)
		fmt.Printf("Destination: %s / %s\n", targetInstance.Name, targetDatabase.Name)
		if opts.PITParsed != nil {
			fmt.Printf("PIT:         %s\n", opts.PITParsed.Format(time.RFC3339))
		}
		if opts.Backup != "" {
			fmt.Printf("Backup:      %s\n", opts.Backup)
		}
		if opts.DumpFile != "" {
			fmt.Printf("Dump File:   %s\n", opts.DumpFile)
		}
		fmt.Println("================================================================================")

		switch mode {
		case restoreFromLiveDB:
			fmt.Printf("Dumping from live source database %s / %s and restoring into %s / %s...\n", sourceInstance.Name, sourceDatabase.Name, targetInstance.Name, targetDatabase.Name)
			dump, err := apiClient.CreateDump(ctx, sourceInstance, sourceDatabase, api.DumpModeStandard, nil)
			if err != nil {
				return fmt.Errorf("dump from live source db: %w", err)
			}
			if err := apiClient.RestoreDump(ctx, targetInstance, targetDatabase, dump); err != nil {
				return fmt.Errorf("restore live dump into destination db: %w", err)
			}
			fmt.Printf("Restore from live db completed successfully using dump: %s\n", dump.Path)
			return nil

		case restoreFromDump:
			fmt.Printf("Restoring from dump file %q into %s / %s...\n", opts.DumpFile, targetInstance.Name, targetDatabase.Name)
			dumpArtifact := api.DumpArtifact{
				Name:         filepath.Base(opts.DumpFile),
				Path:         opts.DumpFile,
				Mode:         api.DumpModeStandard,
				InstanceName: sourceInstance.Name,
				InstanceID:   sourceInstance.ID,
				DatabaseName: sourceDatabase.Name,
				CreatedAt:    time.Now().UTC(),
			}
			if err := apiClient.RestoreDump(ctx, targetInstance, targetDatabase, dumpArtifact); err != nil {
				return fmt.Errorf("restore dump file: %w", err)
			}
			fmt.Printf("Restore completed successfully from file: %s\n", opts.DumpFile)
			return nil

		case restoreFromStackitBackup:
			backups, err := apiClient.GetBackups(ctx, sourceInstance)
			if err != nil {
				return fmt.Errorf("get backups for source instance %q: %w", sourceInstance.Name, err)
			}
			var targetBackup *api.Backup
			for _, b := range backups {
				if strings.EqualFold(b.Name, opts.Backup) {
					targetBackup = &b
					break
				}
			}
			if targetBackup == nil {
				return fmt.Errorf("backup %q not found for source instance %q", opts.Backup, sourceInstance.Name)
			}
			fmt.Printf("Restoring backup %q (%s) into %s / %s...\n", targetBackup.Name, targetBackup.CreatedAt.Format(time.RFC3339), targetInstance.Name, targetDatabase.Name)
			artifact, err := apiClient.RestoreFromPIT(ctx, targetInstance, targetDatabase, targetBackup.CreatedAt)
			if err != nil {
				return fmt.Errorf("restore from backup: %w", err)
			}
			fmt.Printf("Restore from backup completed successfully using dump: %s\n", artifact.Path)
			return nil

		case restoreFromPIT:
			if opts.PITParsed == nil {
				return fmt.Errorf("missing parsed PIT timestamp")
			}
			fmt.Printf("Restoring from PIT datetime %s into %s / %s...\n", opts.PITParsed.Format(time.RFC3339), targetInstance.Name, targetDatabase.Name)
			artifact, err := apiClient.RestoreFromPIT(ctx, targetInstance, targetDatabase, *opts.PITParsed)
			if err != nil {
				return fmt.Errorf("restore from PIT: %w", err)
			}
			fmt.Printf("Restore from PIT completed successfully using dump: %s\n", artifact.Path)
			return nil

		default:
			return fmt.Errorf("unsupported restore mode %q", opts.Mode)
		}

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

	sourceForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[databaseSelection]().
				OptionsFunc(app.getDatabaseOptions, app.databaseSelections).
				Validate(func(s databaseSelection) error {
					if !postgres.HasCredentials(s.Instance.Name) {
						hint := postgres.GetMissingCredentialsHint(s.Instance.Name)
						return fmt.Errorf("instance %q is unavailable: missing %s", s.Instance.Name, hint)
					}
					return nil
				}).
				Value(&app.sourceSelection).
				Title("Please select the source database").
				Height(5),
		),
	)

	if err := sourceForm.Run(); err != nil {
		return err
	}

	actionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[action]().
				Options(
					huh.NewOption("Dump", actionDump),
					huh.NewOption("Restore", actionRestore),
				).
				Value(&app.selectedAction).
				Title("What would you like to do with this database?").
				Description(app.selectedDBHeader()),
		),
	)

	if err := actionForm.Run(); err != nil {
		return err
	}

	switch app.selectedAction {
	case actionDump:
		if err := app.runDumpFlow(ctx); err != nil {
			return err
		}
	case actionRestore:
		destForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[databaseSelection]().
					OptionsFunc(app.getDestinationDatabaseOptions, app.databaseSelections).
					Validate(func(s databaseSelection) error {
						if !postgres.HasCredentials(s.Instance.Name) {
							hint := postgres.GetMissingCredentialsHint(s.Instance.Name)
							return fmt.Errorf("instance %q is unavailable: missing %s", s.Instance.Name, hint)
						}
						return nil
					}).
					Value(&app.destSelection).
					Title("Please select the destination database to restore into").
					Description(app.selectedDBHeader()).
					Height(5),
			),
		)

		if err := destForm.Run(); err != nil {
			return err
		}

		if err := app.runRestoreFlow(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (a *appForm) runDumpFlow(ctx context.Context) error {
	dumpModeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[api.DumpMode]().
				Options(
					huh.NewOption("Dump from live data", api.DumpModeStandard),
					huh.NewOption("Dump from stackit replica", api.DumpModeReplica),
					huh.NewOption("Dump from stackit replica (PIT)", api.DumpModePointInTime),
				).
				Value(&a.selectedDumpMode).
				Title("Please select dump mode").
				Description(a.selectedDBHeader()),
		),
	)

	if err := dumpModeForm.Run(); err != nil {
		return err
	}

	if a.selectedDumpMode == api.DumpModePointInTime {
		if err := a.promptPITTimestamp(); err != nil {
			return err
		}
	}

	var confirm bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Confirm Dump Operation?").
				Description(fmt.Sprintf("%s\n\nExplanation:\n%s", a.selectedDBHeader(), a.buildExplanation())).
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

	var artifact api.DumpArtifact
	err := spinner.New().
		Context(ctx).
		Accessible(false).
		Title("Creating dump...").
		ActionWithErr(func(ctx context.Context) error {
			var pit *time.Time
			if a.selectedDumpMode == api.DumpModePointInTime {
				pit = &a.selectedPIT
			}

			dump, err := a.apiClient.CreateDump(
				ctx,
				a.sourceSelection.Instance,
				a.sourceSelection.Database,
				a.selectedDumpMode,
				pit,
			)
			if err != nil {
				return err
			}
			artifact = dump
			return nil
		}).
		Run()
	if err != nil {
		return err
	}

	fmt.Printf("Dump created for %s / %s: %s\n", a.sourceSelection.Instance.Name, a.sourceSelection.Database.Name, artifact.Path)
	return nil
}

func (a *appForm) runRestoreFlow(ctx context.Context) error {
	restoreModeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[restoreMode]().
				Options(
					huh.NewOption("Restore from live db", restoreFromLiveDB),
					huh.NewOption("Restore from Stackit backup", restoreFromStackitBackup),
					huh.NewOption("Restore from Stackit replica (PIT)", restoreFromPIT),
					huh.NewOption("Restore from existing .dump file", restoreFromDump),
				).
				Value(&a.selectedRestoreMode).
				Title("Please select restore mode").
				Description(a.selectedDBHeader()),
		),
	)
	if err := restoreModeForm.Run(); err != nil {
		return err
	}

	switch a.selectedRestoreMode {
	case restoreFromLiveDB:
		var confirm bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Confirm Restore Operation?").
					Description(fmt.Sprintf("%s\n\nExplanation:\n%s", a.selectedDBHeader(), a.buildExplanation())).
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

		var generatedDump api.DumpArtifact
		err := spinner.New().
			Context(ctx).
			Accessible(false).
			Title("Restoring directly from live source database into destination...").
			ActionWithErr(func(ctx context.Context) error {
				dump, err := a.apiClient.CreateDump(
					ctx,
					a.sourceSelection.Instance,
					a.sourceSelection.Database,
					api.DumpModeStandard,
					nil,
				)
				if err != nil {
					return fmt.Errorf("create dump from live source db: %w", err)
				}
				generatedDump = dump

				if err := a.apiClient.RestoreDump(
					ctx,
					a.destSelection.Instance,
					a.destSelection.Database,
					dump,
				); err != nil {
					return fmt.Errorf("restore live dump into destination db: %w", err)
				}
				return nil
			}).
			Run()
		if err != nil {
			return err
		}

		fmt.Printf(
			"Restore from live db completed for source %s / %s into destination %s / %s using dump: %s\n",
			a.sourceSelection.Instance.Name,
			a.sourceSelection.Database.Name,
			a.destSelection.Instance.Name,
			a.destSelection.Database.Name,
			generatedDump.Path,
		)
		return nil

	case restoreFromDump:
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
					Title("Please select a dump to restore").
					Description(a.selectedDBHeader()).
					Height(5),
			),
		)
		if err := dumpSelectForm.Run(); err != nil {
			return err
		}

		var confirm bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Confirm Restore Operation?").
					Description(fmt.Sprintf("%s\n\nExplanation:\n%s", a.selectedDBHeader(), a.buildExplanation())).
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

		err := spinner.New().
			Context(ctx).
			Accessible(false).
			Title("Restoring dump...").
			ActionWithErr(func(ctx context.Context) error {
				return a.apiClient.RestoreDump(ctx, a.destSelection.Instance, a.destSelection.Database, a.selectedDump)
			}).
			Run()
		if err != nil {
			return err
		}

		fmt.Printf(
			"Restore completed from dump %s into %s / %s\n",
			a.selectedDump.Path,
			a.destSelection.Instance.Name,
			a.destSelection.Database.Name,
		)
		return nil

	case restoreFromStackitBackup:
		if err := a.selectBackupForPIT(); err != nil {
			return err
		}

		var confirm bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Confirm Restore Operation?").
					Description(fmt.Sprintf("%s\n\nExplanation:\n%s", a.selectedDBHeader(), a.buildExplanation())).
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

		var generatedDump api.DumpArtifact
		err := spinner.New().
			Context(ctx).
			Accessible(false).
			Title("Restoring from backup...").
			ActionWithErr(func(ctx context.Context) error {
				dump, err := a.apiClient.RestoreFromPIT(
					ctx,
					a.destSelection.Instance,
					a.destSelection.Database,
					a.selectedBackup.CreatedAt,
				)
				if err != nil {
					return err
				}
				generatedDump = dump
				return nil
			}).
			Run()
		if err != nil {
			return err
		}

		fmt.Printf(
			"Restore from backup completed for source %s / %s into destination %s / %s using dump: %s\n",
			a.sourceSelection.Instance.Name,
			a.sourceSelection.Database.Name,
			a.destSelection.Instance.Name,
			a.destSelection.Database.Name,
			generatedDump.Path,
		)
		return nil

	case restoreFromPIT:
		if err := a.promptPITTimestamp(); err != nil {
			return err
		}

		var confirm bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Confirm Restore Operation?").
					Description(fmt.Sprintf("%s\n\nExplanation:\n%s", a.selectedDBHeader(), a.buildExplanation())).
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

		var generatedDump api.DumpArtifact
		err := spinner.New().
			Context(ctx).
			Accessible(false).
			Title("Restoring from PIT...").
			ActionWithErr(func(ctx context.Context) error {
				dump, err := a.apiClient.RestoreFromPIT(
					ctx,
					a.destSelection.Instance,
					a.destSelection.Database,
					a.selectedPIT,
				)
				if err != nil {
					return err
				}
				generatedDump = dump
				return nil
			}).
			Run()
		if err != nil {
			return err
		}

		fmt.Printf(
			"Restore from PIT completed for source %s / %s into destination %s / %s using dump: %s\n",
			a.sourceSelection.Instance.Name,
			a.sourceSelection.Database.Name,
			a.destSelection.Instance.Name,
			a.destSelection.Database.Name,
			generatedDump.Path,
		)
		return nil

	default:
		return fmt.Errorf("unsupported restore mode %q", a.selectedRestoreMode)
	}
}

func (a *appForm) preloadResources(ctx context.Context) error {
	instances, err := a.apiClient.GetInstances(ctx)
	if err != nil {
		return err
	}

	if len(instances) == 0 {
		return fmt.Errorf("no instances found")
	}

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
	options := make([]huh.Option[databaseSelection], len(a.databaseSelections))
	for i, selection := range a.databaseSelections {
		hasCreds := postgres.HasCredentials(selection.Instance.Name)
		label := fmt.Sprintf("%s / %s", selection.Instance.Name, selection.Database.Name)
		if !hasCreds {
			hint := postgres.GetMissingCredentialsHint(selection.Instance.Name)
			label = fmt.Sprintf("%s / %s (unavailable: missing %s)", selection.Instance.Name, selection.Database.Name, hint)
		}
		options[i] = huh.NewOption(label, selection)
	}
	return options
}

func (a *appForm) getDestinationDatabaseOptions() []huh.Option[databaseSelection] {
	options := make([]huh.Option[databaseSelection], len(a.databaseSelections))
	for i, selection := range a.databaseSelections {
		hasCreds := postgres.HasCredentials(selection.Instance.Name)
		label := fmt.Sprintf("%s / %s", selection.Instance.Name, selection.Database.Name)
		if !hasCreds {
			hint := postgres.GetMissingCredentialsHint(selection.Instance.Name)
			label = fmt.Sprintf("%s / %s (unavailable: missing %s)", selection.Instance.Name, selection.Database.Name, hint)
		}
		options[i] = huh.NewOption(label, selection)
	}
	return options
}

func (a *appForm) getRestoreBackupOptions() []huh.Option[api.Backup] {
	backups := a.backupsByInstance[a.sourceSelection.Instance.ID]
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	options := make([]huh.Option[api.Backup], len(backups))

	for i, backup := range backups {
		label := fmt.Sprintf("%s (%s)", backup.Name, backup.CreatedAt.Format(time.RFC3339))
		options[i] = huh.NewOption(label, backup)
	}

	return options
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

func (a *appForm) selectBackupForPIT() error {
	backups := a.backupsByInstance[a.sourceSelection.Instance.ID]
	if len(backups) == 0 {
		return fmt.Errorf("no backups available for source instance %q", a.sourceSelection.Instance.Name)
	}

	restoreForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[api.Backup]().
				OptionsFunc(a.getRestoreBackupOptions, a.sourceSelection).
				Value(&a.selectedBackup).
				Title("Please select source backup").
				Description(a.selectedDBHeader()).
				Height(5),
		),
	)

	if err := restoreForm.Run(); err != nil {
		return err
	}
	return nil
}

func (a *appForm) promptPITTimestamp() error {
	backups := a.backupsByInstance[a.sourceSelection.Instance.ID]

	if len(backups) > 0 {
		var method pitInputMethod
		methodForm := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[pitInputMethod]().
					Options(
						huh.NewOption("Select from available backup datetimes", pitMethodSelectBackup),
						huh.NewOption("Enter custom datetime", pitMethodEnterCustom),
					).
					Value(&method).
					Title("How would you like to specify the PIT datetime?").
					Description(a.selectedDBHeader()),
			),
		)
		if err := methodForm.Run(); err != nil {
			return err
		}

		if method == pitMethodSelectBackup {
			if err := a.selectBackupForPIT(); err != nil {
				return err
			}
			a.selectedPIT = a.selectedBackup.CreatedAt
			return nil
		}
	}

	var rawInput string
	customForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Enter Point-In-Time datetime (UTC)").
				Description(fmt.Sprintf("%s\nFormat: YYYY-MM-DD HH:MM:SS or RFC3339 (e.g. 2026-08-13 15:00:00)", a.selectedDBHeader())).
				Value(&rawInput).
				Validate(func(val string) error {
					_, err := ParsePITTimestamp(val)
					return err
				}),
		),
	)

	if err := customForm.Run(); err != nil {
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
