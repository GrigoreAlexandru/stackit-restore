package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
)

var LocalInstance = stackit.Instance{
	Name: "local",
	ID:   "local",
}

type LocalProvider struct {
	host            string
	port            int32
	defaultDatabase string
}

func NewLocalProvider(cfg config.Config) *LocalProvider {
	host := cfg.LocalHost
	if strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	port := int32(cfg.LocalPort)
	if port <= 0 {
		port = 5432
	}

	defaultDB := os.Getenv("LOCAL_DATABASE")
	if strings.TrimSpace(defaultDB) == "" {
		defaultDB = "postgres"
	}

	return &LocalProvider{
		host:            host,
		port:            port,
		defaultDatabase: defaultDB,
	}
}

func (p *LocalProvider) Name() string {
	return "local"
}

func (p *LocalProvider) Handles(instance stackit.Instance) bool {
	return strings.EqualFold(strings.TrimSpace(instance.Name), "local") ||
		strings.EqualFold(strings.TrimSpace(instance.ID), "local")
}

func (p *LocalProvider) GetInstances(ctx context.Context) ([]stackit.Instance, error) {
	_ = ctx
	return []stackit.Instance{LocalInstance}, nil
}

func (p *LocalProvider) GetDatabases(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error) {
	_ = ctx
	_ = instance

	dbName := p.defaultDatabase
	if envDB := os.Getenv("LOCAL_DATABASE"); strings.TrimSpace(envDB) != "" {
		dbName = envDB
	}

	return []stackit.Database{
		{
			Name:  dbName,
			ID:    1,
			Owner: "postgres",
		},
	}, nil
}

func (p *LocalProvider) GetBackups(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error) {
	_ = ctx
	_ = instance
	return []stackit.Backup{}, nil
}

func (p *LocalProvider) ResolveEndpoint(ctx context.Context, instance stackit.Instance) (Endpoint, error) {
	_ = ctx
	_ = instance

	host, port, err := postgres.GetLocalEndpoint()
	if err == nil && host != "" && port > 0 {
		return Endpoint{Host: host, Port: port}, nil
	}

	return Endpoint{
		Host: p.host,
		Port: p.port,
	}, nil
}

func (p *LocalProvider) CreateClone(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
	_ = ctx
	_ = instance
	_ = pit
	return stackit.Instance{}, fmt.Errorf("cloning is not supported for local database provider")
}

func (p *LocalProvider) DeleteInstance(ctx context.Context, instance stackit.Instance) error {
	_ = ctx
	_ = instance
	return nil
}

func (p *LocalProvider) SupportsCloning() bool {
	return false
}
