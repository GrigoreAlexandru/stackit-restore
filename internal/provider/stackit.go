package provider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
)

type StackitProvider struct {
	client *stackit.Client
	region string
}

func NewStackitProvider(client *stackit.Client, region string) *StackitProvider {
	if region == "" && client != nil {
		region = client.Region()
	}
	if region == "" {
		region = "eu01"
	}

	return &StackitProvider{
		client: client,
		region: region,
	}
}

func (p *StackitProvider) Name() string {
	return "stackit"
}

func (p *StackitProvider) Handles(instance stackit.Instance) bool {
	return !strings.EqualFold(strings.TrimSpace(instance.Name), "local") &&
		!strings.EqualFold(strings.TrimSpace(instance.ID), "local")
}

func (p *StackitProvider) GetInstances(ctx context.Context) ([]stackit.Instance, error) {
	if p.client == nil {
		return nil, fmt.Errorf("STACKIT client is not configured")
	}
	return p.client.GetInstances(ctx)
}

func (p *StackitProvider) GetDatabases(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error) {
	if p.client == nil {
		return nil, fmt.Errorf("STACKIT client is not configured")
	}
	return p.client.GetDatabases(ctx, instance)
}

func (p *StackitProvider) GetBackups(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error) {
	if p.client == nil {
		return nil, fmt.Errorf("STACKIT client is not configured")
	}
	return p.client.GetBackups(ctx, instance)
}

func (p *StackitProvider) ResolveEndpoint(ctx context.Context, instance stackit.Instance) (Endpoint, error) {
	if p.client == nil {
		return Endpoint{}, fmt.Errorf("STACKIT client is not configured")
	}
	host, port, err := p.client.GetInstanceEndpoint(ctx, instance)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{
		Host: host,
		Port: port,
	}, nil
}

func (p *StackitProvider) CreateClone(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
	if p.client == nil {
		return stackit.Instance{}, fmt.Errorf("STACKIT client is not configured")
	}
	return p.client.CreateClone(ctx, instance, pit)
}

func (p *StackitProvider) DeleteInstance(ctx context.Context, instance stackit.Instance) error {
	if p.client == nil {
		return fmt.Errorf("STACKIT client is not configured")
	}
	return p.client.DeleteInstance(ctx, instance)
}

func (p *StackitProvider) SupportsCloning() bool {
	return true
}
