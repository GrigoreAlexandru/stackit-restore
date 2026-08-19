package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/postgres"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/provider"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
)

type mockProvider struct {
	name            string
	handlesFunc     func(instance stackit.Instance) bool
	getInstances    func(ctx context.Context) ([]stackit.Instance, error)
	getDatabases    func(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error)
	getBackups      func(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error)
	resolveEndpoint func(ctx context.Context, instance stackit.Instance) (provider.Endpoint, error)
	createClone     func(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error)
	deleteInstance  func(ctx context.Context, instance stackit.Instance) error
	supportsCloning bool
}

func (m *mockProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *mockProvider) Handles(instance stackit.Instance) bool {
	if m.handlesFunc != nil {
		return m.handlesFunc(instance)
	}
	return true
}

func (m *mockProvider) GetInstances(ctx context.Context) ([]stackit.Instance, error) {
	if m.getInstances != nil {
		return m.getInstances(ctx)
	}
	return []stackit.Instance{{Name: "test-inst", ID: "id-1"}}, nil
}

func (m *mockProvider) GetDatabases(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error) {
	if m.getDatabases != nil {
		return m.getDatabases(ctx, instance)
	}
	return []stackit.Database{{Name: "test-db"}}, nil
}

func (m *mockProvider) GetBackups(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error) {
	if m.getBackups != nil {
		return m.getBackups(ctx, instance)
	}
	return []stackit.Backup{
		{Name: "b1", CreatedAt: time.Now().Add(-2 * time.Hour), Size: 1024 * 1024 * 1024},
		{Name: "b2", CreatedAt: time.Now().Add(-1 * time.Hour), Size: 2 * 1024 * 1024 * 1024},
	}, nil
}

func (m *mockProvider) ResolveEndpoint(ctx context.Context, instance stackit.Instance) (provider.Endpoint, error) {
	if m.resolveEndpoint != nil {
		return m.resolveEndpoint(ctx, instance)
	}
	return provider.Endpoint{Host: "127.0.0.1", Port: 5432}, nil
}

func (m *mockProvider) CreateClone(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
	if m.createClone != nil {
		return m.createClone(ctx, instance, pit)
	}
	return stackit.Instance{Name: instance.Name + "-temp-clone", ID: "clone-id-123"}, nil
}

func (m *mockProvider) DeleteInstance(ctx context.Context, instance stackit.Instance) error {
	if m.deleteInstance != nil {
		return m.deleteInstance(ctx, instance)
	}
	return nil
}

func (m *mockProvider) SupportsCloning() bool {
	return m.supportsCloning
}

func setupTestClient(t *testing.T, p provider.Provider) (*Client, string) {
	t.Helper()
	tempDir := t.TempDir()
	dumpDir := filepath.Join(tempDir, "dumps")
	if err := os.MkdirAll(dumpDir, 0755); err != nil {
		t.Fatalf("failed to create dump dir: %v", err)
	}

	router := provider.NewRouter(p)
	client := &Client{
		router:    router,
		artifacts: postgres.NewArtifactManager(dumpDir),
	}
	return client, dumpDir
}

func TestClient_GetInstancesAndDatabases(t *testing.T) {
	mockP := &mockProvider{
		getInstances: func(ctx context.Context) ([]stackit.Instance, error) {
			return []stackit.Instance{{Name: "prod", ID: "p-1"}}, nil
		},
		getDatabases: func(ctx context.Context, instance stackit.Instance) ([]stackit.Database, error) {
			return []stackit.Database{{Name: "app_db"}}, nil
		},
	}

	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()

	instances, err := client.GetInstances(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(instances) != 1 || instances[0].Name != "prod" {
		t.Fatalf("unexpected instances: %+v", instances)
	}

	databases, err := client.GetDatabases(ctx, instances[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(databases) != 1 || databases[0].Name != "app_db" {
		t.Fatalf("unexpected databases: %+v", databases)
	}
}

func TestClient_GetBackupsAndLatest(t *testing.T) {
	t1 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	mockP := &mockProvider{
		getBackups: func(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error) {
			return []stackit.Backup{
				{Name: "b1", CreatedAt: t1},
				{Name: "b2", CreatedAt: t2},
			}, nil
		},
	}

	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "prod", ID: "p-1"}

	backups, err := client.GetBackups(ctx, inst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(backups))
	}

	latest, err := client.getLatestBackupTime(ctx, mockP, inst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !latest.Equal(t2) {
		t.Fatalf("expected latest backup time %v, got %v", t2, latest)
	}
}

func TestClient_RestoreDump_EmptyPathRejected(t *testing.T) {
	mockP := &mockProvider{}
	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "prod", ID: "p-1"}
	db := stackit.Database{Name: "app_db"}

	err := client.RestoreDump(ctx, inst, db, DumpArtifact{Path: ""})
	if err == nil {
		t.Fatal("expected error when restoring with empty dump path, got nil")
	}
}

func TestClient_CreateDump_PointInTimeRequiresPIT(t *testing.T) {
	mockP := &mockProvider{supportsCloning: true}
	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "prod", ID: "p-1"}
	db := stackit.Database{Name: "app_db"}

	_, err := client.CreateDump(ctx, inst, db, DumpModePointInTime, nil)
	if err == nil {
		t.Fatal("expected error when PIT is nil in PointInTime mode, got nil")
	}
}

func TestClient_CreateDump_UnsupportedCloning(t *testing.T) {
	mockP := &mockProvider{supportsCloning: false}
	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "local", ID: "local"}
	db := stackit.Database{Name: "app_db"}
	pit := time.Now()

	_, err := client.CreateDump(ctx, inst, db, DumpModeReplica, &pit)
	if err == nil {
		t.Fatal("expected error when provider does not support cloning in Replica mode, got nil")
	}

	_, err = client.CreateDump(ctx, inst, db, DumpModePointInTime, &pit)
	if err == nil {
		t.Fatal("expected error when provider does not support cloning in PIT mode, got nil")
	}
}

func TestClient_CreateCloneAndDeleteInstance(t *testing.T) {
	cloned := false
	deleted := false

	mockP := &mockProvider{
		createClone: func(ctx context.Context, instance stackit.Instance, pit time.Time) (stackit.Instance, error) {
			cloned = true
			return stackit.Instance{Name: "temp-clone", ID: "temp-id"}, nil
		},
		deleteInstance: func(ctx context.Context, instance stackit.Instance) error {
			deleted = true
			return nil
		},
	}

	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "prod", ID: "p-1"}

	clone, err := client.CreateClone(ctx, inst, time.Now())
	if err != nil {
		t.Fatalf("unexpected error creating clone: %v", err)
	}
	if !cloned || clone.ID != "temp-id" {
		t.Fatalf("expected clone to be created with id 'temp-id', got %+v", clone)
	}

	if err := client.DeleteInstance(ctx, clone); err != nil {
		t.Fatalf("unexpected error deleting clone: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleteInstance to be called")
	}
}

func TestClient_ListDumpArtifacts(t *testing.T) {
	mockP := &mockProvider{}
	client, dumpDir := setupTestClient(t, mockP)
	ctx := context.Background()

	// Write a test dump file
	dumpFile := filepath.Join(dumpDir, "test_20260819_120000.dump")
	if err := os.WriteFile(dumpFile, []byte("fake dump content"), 0644); err != nil {
		t.Fatalf("failed to create fake dump file: %v", err)
	}

	artifacts, err := client.ListDumpArtifacts(ctx)
	if err != nil {
		t.Fatalf("unexpected error listing dump artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Path != dumpFile {
		t.Fatalf("expected artifact path %q, got %q", dumpFile, artifacts[0].Path)
	}
}

func TestClient_CreateDump_MissingCredentials(t *testing.T) {
	mockP := &mockProvider{supportsCloning: true}
	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "UnconfiguredInstance", ID: "u-1"}
	db := stackit.Database{Name: "app_db"}

	_, err := client.CreateDump(ctx, inst, db, DumpModeStandard, nil)
	if err == nil {
		t.Fatal("expected error when credentials for instance are missing, got nil")
	}
}

func TestClient_GetLatestBackupTime_EmptyBackups(t *testing.T) {
	mockP := &mockProvider{
		getBackups: func(ctx context.Context, instance stackit.Instance) ([]stackit.Backup, error) {
			return []stackit.Backup{}, nil
		},
	}
	client, _ := setupTestClient(t, mockP)
	ctx := context.Background()
	inst := stackit.Instance{Name: "prod", ID: "p-1"}

	_, err := client.getLatestBackupTime(ctx, mockP, inst)
	if err == nil {
		t.Fatal("expected error when no backups are available, got nil")
	}
}

func TestClient_NewClient_NoAuth(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.Config{
		DumpDir: tempDir,
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating client with local-only config: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestCheckPreflightTools(t *testing.T) {
	// Should not panic or crash
	_ = CheckPreflightTools()
}


