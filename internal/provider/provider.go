package provider

import (
	"context"
	"fmt"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
	"time"
)

type Endpoint struct {
	Host string
	Port int32
}

type Provider interface {
	Name() string
	Handles(instance stackit.Instance) bool
	GetInstances(ctx context.Context) ([]stackit.Instance, error)
	GetDatabases(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error)
	GetBackups(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error)
	ResolveEndpoint(ctx context.Context, instance stackit.Instance) (Endpoint, error)
	CreateClone(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error)
	DeleteInstance(ctx context.Context, instance stackit.Instance) error
	SupportsCloning() bool
}

type Router struct {
	providers []Provider
}

func NewRouter(providers ...Provider) *Router {
	var valid []Provider
	for _, p := range providers {
		if p != nil {
			valid = append(valid, p)
		}
	}
	return &Router{providers: valid}
}

func (r *Router) GetInstances(ctx context.Context) ([]stackit.Instance, error) {
	var allInstances []stackit.Instance
	for _, p := range r.providers {
		instances, err := p.GetInstances(ctx)
		if err != nil {
			return nil, fmt.Errorf("provider %q get instances: %w", p.Name(), err)
		}
		allInstances = append(allInstances, instances...)
	}
	return allInstances, nil
}

func (r *Router) Route(instance stackit.Instance) (Provider, error) {
	for _, p := range r.providers {
		if p.Handles(instance) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no provider found for instance %q (id: %q)", instance.Name, instance.ID)
}
