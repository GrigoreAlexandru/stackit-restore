package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/provider"
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

var InstanceLocal = provider.LocalInstance

type Client struct {
	router    *provider.Router
	artifacts *postgres.ArtifactManager
}

func NewClient(cfg config.Config) (*Client, error) {
	localProvider := provider.NewLocalProvider(cfg)

	var stackitProvider *provider.StackitProvider
	if cfg.HasAuth() && cfg.ProjectID != "" {
		st, err := stackit.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		stackitProvider = provider.NewStackitProvider(st, cfg.Region)
	}

	router := provider.NewRouter(localProvider, stackitProvider)

	return &Client{
		router:    router,
		artifacts: postgres.NewArtifactManager(cfg.DumpDir),
	}, nil
}

func CheckPreflightTools() error {
	return postgres.CheckPreflightTools()
}

func (c *Client) GetInstances(ctx context.Context) ([]Instance, error) {
	return c.router.GetInstances(ctx)
}

func (c *Client) GetBackups(ctx context.Context, instance Instance) ([]Backup, error) {
	p, err := c.router.Route(instance)
	if err != nil {
		return nil, err
	}
	return p.GetBackups(ctx, instance)
}

func (c *Client) GetDatabases(ctx context.Context, instance Instance) ([]Database, error) {
	p, err := c.router.Route(instance)
	if err != nil {
		return nil, err
	}
	return p.GetDatabases(ctx, instance)
}

func (c *Client) CreateClone(ctx context.Context, instance Instance, pit time.Time) (Instance, error) {
	p, err := c.router.Route(instance)
	if err != nil {
		return Instance{}, err
	}
	return p.CreateClone(ctx, instance, pit)
}

func (c *Client) DeleteInstance(ctx context.Context, instance Instance) error {
	p, err := c.router.Route(instance)
	if err != nil {
		return err
	}
	return p.DeleteInstance(ctx, instance)
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
	p, err := c.router.Route(instance)
	if err != nil {
		return DumpArtifact{}, err
	}

	switch mode {
	case DumpModeStandard:
		return c.createDumpFromInstance(ctx, p, instance, database, mode)

	case DumpModeReplica:
		if !p.SupportsCloning() {
			return DumpArtifact{}, fmt.Errorf("dump from replica is not supported by provider %q", p.Name())
		}

		latestBackupTime, err := c.getLatestBackupTime(ctx, p, instance)
		if err != nil {
			return DumpArtifact{}, err
		}

		clone, err := p.CreateClone(ctx, instance, latestBackupTime)
		if err != nil {
			return DumpArtifact{}, err
		}

		cloneProvider, err := c.router.Route(clone)
		if err != nil {
			cloneProvider = p
		}

		dump, dumpErr := c.createDumpFromInstance(ctx, cloneProvider, clone, database, mode)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		deleteErr := p.DeleteInstance(cleanupCtx, clone)
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
		if !p.SupportsCloning() {
			return DumpArtifact{}, fmt.Errorf("dump from PIT replica is not supported by provider %q", p.Name())
		}
		if pit == nil {
			return DumpArtifact{}, fmt.Errorf("point in time is required for %q mode", mode)
		}

		clone, err := p.CreateClone(ctx, instance, *pit)
		if err != nil {
			return DumpArtifact{}, err
		}

		cloneProvider, err := c.router.Route(clone)
		if err != nil {
			cloneProvider = p
		}

		dump, dumpErr := c.createDumpFromInstance(ctx, cloneProvider, clone, database, mode)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		deleteErr := p.DeleteInstance(cleanupCtx, clone)
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
	p, err := c.router.Route(instance)
	if err != nil {
		return err
	}

	endpoint, err := p.ResolveEndpoint(ctx, instance)
	if err != nil {
		return err
	}

	credentials, err := postgres.ResolveCredentials(instance.Name)
	if err != nil {
		return err
	}

	return postgres.RunPgRestore(ctx, endpoint.Host, endpoint.Port, database.Name, dump.Path, credentials)
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

func (c *Client) getLatestBackupTime(ctx context.Context, p provider.Provider, instance Instance) (time.Time, error) {
	backups, err := p.GetBackups(ctx, instance)
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
	p provider.Provider,
	instance Instance,
	database Database,
	mode DumpMode,
) (DumpArtifact, error) {
	endpoint, err := p.ResolveEndpoint(ctx, instance)
	if err != nil {
		return DumpArtifact{}, err
	}

	credentials, err := postgres.ResolveCredentials(instance.Name)
	if err != nil {
		return DumpArtifact{}, err
	}

	artifact := c.artifacts.NewDumpArtifact(instance.ID, instance.Name, database.Name, mode)

	if err := postgres.RunPgDump(ctx, endpoint.Host, endpoint.Port, database.Name, artifact.Path, credentials); err != nil {
		return DumpArtifact{}, err
	}

	if err := c.artifacts.WriteMetadata(artifact); err != nil {
		return DumpArtifact{}, err
	}

	return artifact, nil
}
