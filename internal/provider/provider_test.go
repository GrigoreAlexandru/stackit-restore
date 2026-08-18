package provider

import (
	"context"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
)

func TestLocalProvider_HandlesAndEndpoints(t *testing.T) {
	cfg := config.Config{
		LocalHost: "127.0.0.1",
		LocalPort: 5434,
		LocalDB:   "custom_local_db",
	}

	lp := NewLocalProvider(cfg)
	if lp.Name() != "local" {
		t.Fatalf("expected name 'local', got %q", lp.Name())
	}

	if !lp.Handles(stackit.Instance{Name: "local", ID: "local"}) {
		t.Fatal("expected LocalProvider to handle local instance")
	}
	if !lp.Handles(stackit.Instance{Name: "LOCAL", ID: "123"}) {
		t.Fatal("expected LocalProvider to handle uppercase LOCAL name")
	}
	if lp.Handles(stackit.Instance{Name: "Production", ID: "inst-1"}) {
		t.Fatal("expected LocalProvider to reject non-local instance")
	}

	instances, err := lp.GetInstances(context.Background())
	if err != nil || len(instances) != 1 || instances[0].Name != "local" {
		t.Fatalf("unexpected instances from LocalProvider: %v, %v", instances, err)
	}

	databases, err := lp.GetDatabases(context.Background(), LocalInstance)
	if err != nil || len(databases) != 1 || databases[0].Name != "custom_local_db" {
		t.Fatalf("unexpected databases from LocalProvider: %v, %v", databases, err)
	}

	backups, err := lp.GetBackups(context.Background(), LocalInstance)
	if err != nil || len(backups) != 0 {
		t.Fatalf("expected empty backups from LocalProvider: %v, %v", backups, err)
	}

	endpoint, err := lp.ResolveEndpoint(context.Background(), LocalInstance)
	if err != nil {
		t.Fatalf("unexpected error resolving endpoint: %v", err)
	}
	if endpoint.Host != "127.0.0.1" || endpoint.Port != 5434 {
		t.Fatalf("expected endpoint 127.0.0.1:5434, got %+v", endpoint)
	}

	if lp.SupportsCloning() {
		t.Fatal("expected SupportsCloning to be false for LocalProvider")
	}

	_, cloneErr := lp.CreateClone(context.Background(), LocalInstance, time.Now())
	if cloneErr == nil {
		t.Fatal("expected error on LocalProvider.CreateClone, got nil")
	}
}

func TestStackitProvider_HandlesAndEndpoint(t *testing.T) {
	sp := NewStackitProvider(nil, "eu01")
	if sp.Name() != "stackit" {
		t.Fatalf("expected name 'stackit', got %q", sp.Name())
	}

	if sp.Handles(stackit.Instance{Name: "local", ID: "local"}) {
		t.Fatal("expected StackitProvider to reject local instance")
	}
	if !sp.Handles(stackit.Instance{Name: "Production", ID: "inst-prod-999"}) {
		t.Fatal("expected StackitProvider to handle cloud instance")
	}

	endpoint, err := sp.ResolveEndpoint(context.Background(), stackit.Instance{Name: "Production", ID: "inst-prod-999"})
	if err != nil {
		t.Fatalf("unexpected error resolving endpoint: %v", err)
	}
	expectedHost := "inst-prod-999.postgresql.eu01.onstackit.cloud"
	if endpoint.Host != expectedHost || endpoint.Port != 5432 {
		t.Fatalf("expected endpoint %s:5432, got %+v", expectedHost, endpoint)
	}

	if !sp.SupportsCloning() {
		t.Fatal("expected SupportsCloning to be true for StackitProvider")
	}
}

func TestRouter_Routing(t *testing.T) {
	lp := NewLocalProvider(config.Config{LocalHost: "localhost", LocalPort: 5432, LocalDB: "postgres"})
	sp := NewStackitProvider(nil, "eu01")

	router := NewRouter(lp, sp)

	pLocal, err := router.Route(stackit.Instance{Name: "local", ID: "local"})
	if err != nil || pLocal.Name() != "local" {
		t.Fatalf("expected route to local provider, got %v, err: %v", pLocal, err)
	}

	pCloud, err := router.Route(stackit.Instance{Name: "Production", ID: "prod-1"})
	if err != nil || pCloud.Name() != "stackit" {
		t.Fatalf("expected route to stackit provider, got %v, err: %v", pCloud, err)
	}
}
