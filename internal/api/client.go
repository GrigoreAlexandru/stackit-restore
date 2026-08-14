package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
)

type Instance = stackit.Instance
type Backup = stackit.Backup
type Database = stackit.Database
type DumpMode = postgres.DumpMode

const (
	DumpModeStandard    = postgres.DumpModeStandard
	DumpModeReplica     = postgres.DumpModeReplica
	DumpModePointInTime = postgres.DumpModePointInTime
)

type DumpArtifact = postgres.DumpArtifact

type Client struct {
	stackit   *stackit.Client
	artifacts *postgres.ArtifactManager
}

func NewClient(cfg config.Config) (*Client, error) {
	st, err := stackit.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		stackit:   st,
		artifacts: postgres.NewArtifactManager(cfg.DumpDir),
	}, nil
}

func CheckPreflightTools() error {
	return postgres.CheckPreflightTools()
}

func (c *Client) GetInstances(ctx context.Context) ([]Instance, error) {
	return c.stackit.GetInstances(ctx)
}

func (c *Client) GetBackups(ctx context.Context, instance Instance) ([]Backup, error) {
	return c.stackit.GetBackups(ctx, instance)
}

func (c *Client) GetDatabases(ctx context.Context, instance Instance) ([]Database, error) {
	return c.stackit.GetDatabases(ctx, instance)
}

func (c *Client) CreateClone(ctx context.Context, instance Instance, pit time.Time) (Instance, error) {
	return c.stackit.CreateClone(ctx, instance, pit)
}

func (c *Client) DeleteInstance(ctx context.Context, instance Instance) error {
	return c.stackit.DeleteInstance(ctx, instance)
}

func (c *Client) ListDumpArtifacts(ctx context.Context) ([]DumpArtifact, error) {
	_ = ctx
	return c.artifacts.ListDumpArtifacts()
}

func (c *Client) CreateDump(
	ctx context.Context,
	instance Instance,
	database Database,
	mode DumpMode,
	pit *time.Time,
) (DumpArtifact, error) {
	switch mode {
	case DumpModeStandard:
		return c.createDumpFromInstance(ctx, instance, database, mode)

	case DumpModeReplica:
		latestBackupTime, err := c.getLatestBackupTime(ctx, instance)
		if err != nil {
			return DumpArtifact{}, err
		}

		clone, err := c.stackit.CreateClone(ctx, instance, latestBackupTime)
		if err != nil {
			return DumpArtifact{}, err
		}

		dump, dumpErr := c.createDumpFromInstance(ctx, clone, database, mode)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		deleteErr := c.stackit.DeleteInstance(cleanupCtx, clone)
		cancel()

		if dumpErr != nil && deleteErr != nil {
			return DumpArtifact{}, errors.Join(
				dumpErr,
				fmt.Errorf("delete temporary clone instance %q: %w", clone.Name, deleteErr),
			)
		}
		if dumpErr != nil {
			return DumpArtifact{}, dumpErr
		}
		if deleteErr != nil {
			return DumpArtifact{}, fmt.Errorf("delete temporary clone instance %q: %w", clone.Name, deleteErr)
		}

		return dump, nil

	case DumpModePointInTime:
		if pit == nil {
			return DumpArtifact{}, fmt.Errorf("point in time is required for %q mode", mode)
		}

		clone, err := c.stackit.CreateClone(ctx, instance, *pit)
		if err != nil {
			return DumpArtifact{}, err
		}

		dump, dumpErr := c.createDumpFromInstance(ctx, clone, database, mode)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		deleteErr := c.stackit.DeleteInstance(cleanupCtx, clone)
		cancel()

		if dumpErr != nil && deleteErr != nil {
			return DumpArtifact{}, errors.Join(
				dumpErr,
				fmt.Errorf("delete temporary clone instance %q: %w", clone.Name, deleteErr),
			)
		}
		if dumpErr != nil {
			return DumpArtifact{}, dumpErr
		}
		if deleteErr != nil {
			return DumpArtifact{}, fmt.Errorf("delete temporary clone instance %q: %w", clone.Name, deleteErr)
		}

		return dump, nil

	default:
		return DumpArtifact{}, fmt.Errorf("unsupported dump mode %q", mode)
	}
}

func (c *Client) RestoreDump(
	ctx context.Context,
	instance Instance,
	database Database,
	dump DumpArtifact,
) error {
	host, port, err := c.stackit.GetInstanceEndpoint(ctx, instance)
	if err != nil {
		return err
	}

	credentials, err := postgres.ReadCredentials()
	if err != nil {
		return err
	}

	return postgres.RunPgRestore(ctx, host, port, database.Name, dump.Path, credentials)
}

func (c *Client) RestoreFromPIT(
	ctx context.Context,
	instance Instance,
	database Database,
	pit time.Time,
) (DumpArtifact, error) {
	dump, err := c.CreateDump(ctx, instance, database, DumpModePointInTime, &pit)
	if err != nil {
		return DumpArtifact{}, err
	}

	if err := c.RestoreDump(ctx, instance, database, dump); err != nil {
		return DumpArtifact{}, err
	}

	return dump, nil
}

func (c *Client) getLatestBackupTime(ctx context.Context, instance Instance) (time.Time, error) {
	backups, err := c.stackit.GetBackups(ctx, instance)
	if err != nil {
		return time.Time{}, err
	}
	if len(backups) == 0 {
		return time.Time{}, fmt.Errorf("no backups available for instance %q", instance.Name)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups[0].CreatedAt, nil
}

func (c *Client) createDumpFromInstance(
	ctx context.Context,
	instance Instance,
	database Database,
	mode DumpMode,
) (DumpArtifact, error) {
	host, port, err := c.stackit.GetInstanceEndpoint(ctx, instance)
	if err != nil {
		return DumpArtifact{}, err
	}

	credentials, err := postgres.ReadCredentials()
	if err != nil {
		return DumpArtifact{}, err
	}

	artifact := c.artifacts.NewDumpArtifact(instance.ID, instance.Name, database.Name, mode)

	if err := postgres.RunPgDump(ctx, host, port, database.Name, artifact.Path, credentials); err != nil {
		return DumpArtifact{}, err
	}

	if err := c.artifacts.WriteMetadata(artifact); err != nil {
		return DumpArtifact{}, err
	}

	return artifact, nil
}
