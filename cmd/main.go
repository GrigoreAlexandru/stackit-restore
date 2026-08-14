package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/cli"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	opts, err := cli.ParseOptions(os.Args[1:])
	if err != nil {
		return err
	}

	if opts.Help {
		cli.PrintUsage(os.Stdout)
		return nil
	}

	cfg, err := config.Load()
	if err == nil && cfg.ProjectID != "" && cfg.ServiceAccountToken != "" {
		if err := api.CheckPreflightTools(); err != nil {
			return err
		}
		apiClient, err := api.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("initialize STACKIT API client: %w", err)
		}
		return cli.ExecuteWithOptions(ctx, apiClient, opts)
	}

	return cli.ExecuteWithOptions(ctx, newDummyAPI(), opts)
}

type DummyAPI struct {
	instances []api.Instance
	databases map[string][]api.Database
	backups   map[string][]api.Backup
	dumps     []api.DumpArtifact
}

func newDummyAPI() *DummyAPI {
	t0 := time.Date(2026, time.January, 12, 8, 0, 0, 0, time.UTC)

	instances := []api.Instance{
		{Name: "Production", ID: "instance-prod-001"},
		{Name: "Staging", ID: "instance-stg-001"},
		{Name: "Development", ID: "instance-dev-001"},
	}

	databases := map[string][]api.Database{
		"instance-prod-001": {
			{Name: "app_prod", ID: 101, Owner: "app_owner"},
			{Name: "audit_prod", ID: 102, Owner: "audit_owner"},
		},
		"instance-stg-001": {
			{Name: "app_stg", ID: 201, Owner: "app_owner"},
			{Name: "analytics_stg", ID: 202, Owner: "analytics_owner"},
		},
		"instance-dev-001": {
			{Name: "app_dev", ID: 301, Owner: "developer"},
		},
	}

	backups := map[string][]api.Backup{
		"instance-prod-001": {
			{Name: "prod-auto-20260112", CreatedAt: t0.Add(24 * time.Hour)},
			{Name: "prod-auto-20260113", CreatedAt: t0.Add(48 * time.Hour)},
			{Name: "prod-manual-incident-a", CreatedAt: t0.Add(72 * time.Hour)},
		},
		"instance-stg-001": {
			{Name: "stg-auto-20260112", CreatedAt: t0.Add(20 * time.Hour)},
			{Name: "stg-auto-20260113", CreatedAt: t0.Add(44 * time.Hour)},
		},
		"instance-dev-001": {
			{Name: "dev-auto-20260112", CreatedAt: t0.Add(12 * time.Hour)},
		},
	}

	dumps := []api.DumpArtifact{
		{
			Name:         "20260113T090000Z__dump__instance-prod-001__app_prod.dump",
			Path:         "/tmp/dummy-dumps/20260113T090000Z__dump__instance-prod-001__app_prod.dump",
			Mode:         api.DumpModeStandard,
			InstanceName: "Production",
			InstanceID:   "instance-prod-001",
			DatabaseName: "app_prod",
			CreatedAt:    t0.Add(49 * time.Hour),
		},
		{
			Name:         "20260114T110000Z__dump_from_replica__instance-stg-001__app_stg.dump",
			Path:         "/tmp/dummy-dumps/20260114T110000Z__dump_from_replica__instance-stg-001__app_stg.dump",
			Mode:         api.DumpModeReplica,
			InstanceName: "Staging",
			InstanceID:   "instance-stg-001",
			DatabaseName: "app_stg",
			CreatedAt:    t0.Add(75 * time.Hour),
		},
	}

	return &DummyAPI{
		instances: instances,
		databases: databases,
		backups:   backups,
		dumps:     dumps,
	}
}

func (d *DummyAPI) GetInstances(ctx context.Context) ([]api.Instance, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(250 * time.Millisecond):
	}

	return slices.Clone(d.instances), nil
}

func (d *DummyAPI) GetBackups(ctx context.Context, instance api.Instance) ([]api.Backup, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	backups, ok := d.backups[instance.ID]
	if !ok {
		return nil, fmt.Errorf("no dummy backups found for instance %q", instance.Name)
	}
	return slices.Clone(backups), nil
}

func (d *DummyAPI) GetDatabases(ctx context.Context, instance api.Instance) ([]api.Database, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	databases, ok := d.databases[instance.ID]
	if !ok {
		return nil, fmt.Errorf("no dummy databases found for instance %q", instance.Name)
	}
	return slices.Clone(databases), nil
}

func (d *DummyAPI) ListDumpArtifacts(ctx context.Context) ([]api.DumpArtifact, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return slices.Clone(d.dumps), nil
}

func (d *DummyAPI) CreateDump(
	ctx context.Context,
	instance api.Instance,
	database api.Database,
	mode api.DumpMode,
	pit *time.Time,
) (api.DumpArtifact, error) {
	if err := d.validateDatabaseSelection(instance, database); err != nil {
		return api.DumpArtifact{}, err
	}

	switch mode {
	case api.DumpModeStandard, api.DumpModeReplica:
	case api.DumpModePointInTime:
		if pit == nil {
			return api.DumpArtifact{}, fmt.Errorf("dummy PIT dump requires point in time")
		}
	default:
		return api.DumpArtifact{}, fmt.Errorf("unsupported dummy dump mode %q", mode)
	}

	select {
	case <-ctx.Done():
		return api.DumpArtifact{}, ctx.Err()
	case <-time.After(450 * time.Millisecond):
	}

	createdAt := time.Date(2026, time.January, 20, 10, 0, 0, 0, time.UTC).Add(
		time.Duration(len(d.dumps)) * time.Hour,
	)
	fileName := fmt.Sprintf(
		"%s__%s__%s__%s.dump",
		createdAt.Format("20060102T150405Z"),
		mode,
		instance.ID,
		database.Name,
	)

	artifact := api.DumpArtifact{
		Name:         fileName,
		Path:         "/tmp/dummy-dumps/" + fileName,
		Mode:         mode,
		InstanceName: instance.Name,
		InstanceID:   instance.ID,
		DatabaseName: database.Name,
		CreatedAt:    createdAt,
	}

	d.dumps = append(d.dumps, artifact)
	return artifact, nil
}

func (d *DummyAPI) RestoreDump(
	ctx context.Context,
	instance api.Instance,
	database api.Database,
	dump api.DumpArtifact,
) error {
	if err := d.validateDatabaseSelection(instance, database); err != nil {
		return err
	}

	found := slices.ContainsFunc(d.dumps, func(item api.DumpArtifact) bool {
		return item.Name == dump.Name
	})
	if !found {
		return fmt.Errorf("dummy dump %q does not exist", dump.Name)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(350 * time.Millisecond):
	}

	return nil
}

func (d *DummyAPI) RestoreFromPIT(
	ctx context.Context,
	instance api.Instance,
	database api.Database,
	pit time.Time,
) (api.DumpArtifact, error) {
	dump, err := d.CreateDump(ctx, instance, database, api.DumpModePointInTime, &pit)
	if err != nil {
		return api.DumpArtifact{}, err
	}

	if err := d.RestoreDump(ctx, instance, database, dump); err != nil {
		return api.DumpArtifact{}, err
	}

	return dump, nil
}

func (d *DummyAPI) validateDatabaseSelection(instance api.Instance, database api.Database) error {
	databases, ok := d.databases[instance.ID]
	if !ok {
		return fmt.Errorf("unknown dummy instance %q", instance.Name)
	}

	isKnown := slices.ContainsFunc(databases, func(item api.Database) bool {
		return item.ID == database.ID && strings.EqualFold(item.Name, database.Name)
	})
	if !isKnown {
		return errors.New("selected database is not part of selected dummy instance")
	}

	return nil
}
