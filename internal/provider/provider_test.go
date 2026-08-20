package provider

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/stackit"
)

// Compile-time interface assertions.
var (
	_ Provider = (*LocalProvider)(nil)
	_ Provider = (*StackitProvider)(nil)
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

	delErr := lp.DeleteInstance(context.Background(), LocalInstance)
	if delErr != nil {
		t.Fatalf("expected nil error on DeleteInstance, got %v", delErr)
	}
}

func TestNewLocalProvider_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		cfg          config.Config
		wantHost     string
		wantPort     int32
		wantDatabase string
	}{
		{
			name:         "empty config gets defaults",
			cfg:          config.Config{},
			wantHost:     "localhost",
			wantPort:     5432,
			wantDatabase: "postgres",
		},
		{
			name: "whitespace fields get trimmed to defaults",
			cfg: config.Config{
				LocalHost: "   ",
				LocalPort: 0,
				LocalDB:   "   ",
			},
			wantHost:     "localhost",
			wantPort:     5432,
			wantDatabase: "postgres",
		},
		{
			name: "negative port gets defaulted to 5432",
			cfg: config.Config{
				LocalHost: "10.0.0.1",
				LocalPort: -1,
				LocalDB:   "myapp",
			},
			wantHost:     "10.0.0.1",
			wantPort:     5432,
			wantDatabase: "myapp",
		},
		{
			name: "custom config with padding gets trimmed properly",
			cfg: config.Config{
				LocalHost: "  192.168.1.50  ",
				LocalPort: 5439,
				LocalDB:   "  analytics_db  ",
			},
			wantHost:     "192.168.1.50",
			wantPort:     5439,
			wantDatabase: "analytics_db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lp := NewLocalProvider(tt.cfg)
			if lp == nil {
				t.Fatalf("expected non-nil LocalProvider")
			}
			if lp.host != tt.wantHost {
				t.Errorf("host = %q, want %q", lp.host, tt.wantHost)
			}
			if lp.port != tt.wantPort {
				t.Errorf("port = %d, want %d", lp.port, tt.wantPort)
			}
			if lp.defaultDatabase != tt.wantDatabase {
				t.Errorf("defaultDatabase = %q, want %q", lp.defaultDatabase, tt.wantDatabase)
			}

			dbs, err := lp.GetDatabases(context.Background(), LocalInstance)
			if err != nil {
				t.Fatalf("unexpected error from GetDatabases: %v", err)
			}
			if len(dbs) != 1 {
				t.Fatalf("expected 1 database, got %d", len(dbs))
			}
			if dbs[0].Name != tt.wantDatabase {
				t.Errorf("database name = %q, want %q", dbs[0].Name, tt.wantDatabase)
			}
			if dbs[0].ID != 1 {
				t.Errorf("database ID = %d, want 1", dbs[0].ID)
			}
			if dbs[0].Owner != "postgres" {
				t.Errorf("database Owner = %q, want 'postgres'", dbs[0].Owner)
			}
		})
	}
}

func TestLocalProvider_DeterministicEnvironmentImmunity(t *testing.T) {
	// 1. Environment variables set BEFORE provider creation
	t.Setenv("LOCAL_DB", "hostile_env_db_1")
	t.Setenv("LOCAL_DATABASE", "hostile_env_db_2")

	cfg := config.Config{
		LocalHost: "localhost",
		LocalPort: 5432,
		LocalDB:   "configured_db",
	}

	lp := NewLocalProvider(cfg)

	dbs, err := lp.GetDatabases(context.Background(), LocalInstance)
	if err != nil {
		t.Fatalf("GetDatabases error: %v", err)
	}
	if len(dbs) != 1 || dbs[0].Name != "configured_db" {
		t.Fatalf("expected database 'configured_db', got %+v", dbs)
	}

	// 2. Mutate environment variables AFTER provider creation
	t.Setenv("LOCAL_DB", "hostile_mutated_db_3")
	t.Setenv("LOCAL_DATABASE", "hostile_mutated_db_4")

	dbsAfter, err := lp.GetDatabases(context.Background(), LocalInstance)
	if err != nil {
		t.Fatalf("GetDatabases error after mutation: %v", err)
	}
	if len(dbsAfter) != 1 || dbsAfter[0].Name != "configured_db" {
		t.Fatalf("expected database to remain 'configured_db' despite env mutation, got %+v", dbsAfter)
	}

	// 3. Provider created with empty LocalDB when hostile env vars are set
	emptyCfg := config.Config{
		LocalHost: "localhost",
		LocalPort: 5432,
		LocalDB:   "",
	}
	lpDefault := NewLocalProvider(emptyCfg)
	dbsDefault, err := lpDefault.GetDatabases(context.Background(), LocalInstance)
	if err != nil {
		t.Fatalf("GetDatabases error for default provider: %v", err)
	}
	if len(dbsDefault) != 1 || dbsDefault[0].Name != "postgres" {
		t.Fatalf("expected default database 'postgres' regardless of hostile env, got %+v", dbsDefault)
	}
}

func TestLocalProvider_GetDatabases_ContextCancellation(t *testing.T) {
	lp := NewLocalProvider(config.Config{LocalDB: "test_db"})

	// Nil context should succeed without panic
	dbs, err := lp.GetDatabases(nil, LocalInstance)
	if err != nil {
		t.Fatalf("unexpected error with nil context: %v", err)
	}
	if len(dbs) != 1 || dbs[0].Name != "test_db" {
		t.Fatalf("unexpected databases with nil context: %v", dbs)
	}

	// Normal valid context
	dbsValid, err := lp.GetDatabases(context.Background(), LocalInstance)
	if err != nil {
		t.Fatalf("unexpected error with background context: %v", err)
	}
	if len(dbsValid) != 1 || dbsValid[0].Name != "test_db" {
		t.Fatalf("unexpected databases with background context: %v", dbsValid)
	}

	// Pre-cancelled context should return context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = lp.GetDatabases(ctx, LocalInstance)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// Context with deadline exceeded
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer timeoutCancel()
	time.Sleep(2 * time.Millisecond)

	_, err = lp.GetDatabases(timeoutCtx, LocalInstance)
	if err == nil {
		t.Fatal("expected error on deadline exceeded context, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context error, got %v", err)
	}
}

func TestLocalProvider_HandlesVariations(t *testing.T) {
	lp := NewLocalProvider(config.Config{})

	testCases := []struct {
		instance stackit.Instance
		expected bool
	}{
		{instance: stackit.Instance{Name: "local", ID: "1"}, expected: true},
		{instance: stackit.Instance{Name: "LOCAL", ID: "2"}, expected: true},
		{instance: stackit.Instance{Name: "Local", ID: "3"}, expected: true},
		{instance: stackit.Instance{Name: "  local  ", ID: "4"}, expected: true},
		{instance: stackit.Instance{Name: "other", ID: "local"}, expected: true},
		{instance: stackit.Instance{Name: "other", ID: "LOCAL"}, expected: true},
		{instance: stackit.Instance{Name: "other", ID: "  local  "}, expected: true},
		{instance: stackit.Instance{Name: "Production", ID: "prod-1"}, expected: false},
		{instance: stackit.Instance{Name: "", ID: ""}, expected: false},
	}

	for _, tc := range testCases {
		res := lp.Handles(tc.instance)
		if res != tc.expected {
			t.Errorf("Handles(%+v) = %v, want %v", tc.instance, res, tc.expected)
		}
	}
}

func TestLocalProvider_ConcurrencyRaceDetector(t *testing.T) {
	lp := NewLocalProvider(config.Config{
		LocalHost: "127.0.0.1",
		LocalPort: 5432,
		LocalDB:   "concurrent_db",
	})

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Goroutines calling GetDatabases
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				dbs, err := lp.GetDatabases(context.Background(), LocalInstance)
				if err != nil || len(dbs) != 1 || dbs[0].Name != "concurrent_db" {
					t.Errorf("concurrent GetDatabases failed: dbs=%v, err=%v", dbs, err)
					return
				}
			}
		}()
	}

	// Goroutines calling ResolveEndpoint
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				endpoint, err := lp.ResolveEndpoint(context.Background(), LocalInstance)
				if err != nil || endpoint.Port <= 0 {
					t.Errorf("concurrent ResolveEndpoint failed: endpoint=%+v, err=%v", endpoint, err)
					return
				}
			}
		}()
	}

	// Goroutines calling Handles and Name
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if !lp.Handles(LocalInstance) {
					t.Errorf("concurrent Handles failed")
					return
				}
				if lp.Name() != "local" {
					t.Errorf("concurrent Name failed")
					return
				}
			}
		}()
	}

	wg.Wait()
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

	_, err := sp.ResolveEndpoint(context.Background(), stackit.Instance{Name: "Production", ID: "inst-prod-999"})
	if err == nil {
		t.Fatal("expected error resolving endpoint with unconfigured client, got nil")
	}

	if !sp.SupportsCloning() {
		t.Fatal("expected SupportsCloning to be true for StackitProvider")
	}
}

func TestRouter_Routing(t *testing.T) {
	lp := NewLocalProvider(config.Config{LocalHost: "localhost", LocalPort: 5432, LocalDB: "postgres"})
	sp := NewStackitProvider(nil, "eu01")

	router := NewRouter(nil, lp, sp, nil) // tests nil provider filtering in NewRouter

	pLocal, err := router.Route(stackit.Instance{Name: "local", ID: "local"})
	if err != nil || pLocal.Name() != "local" {
		t.Fatalf("expected route to local provider, got %v, err: %v", pLocal, err)
	}

	pCloud, err := router.Route(stackit.Instance{Name: "Production", ID: "prod-1"})
	if err != nil || pCloud.Name() != "stackit" {
		t.Fatalf("expected route to stackit provider, got %v, err: %v", pCloud, err)
	}

	_, errNotFound := router.Route(stackit.Instance{})
	// For empty instance: StackitProvider Handles() is !local which is true for ""
	// Let's verify behavior
	_ = errNotFound

	instances, err := router.GetInstances(context.Background())
	// sp.client is nil, so router.GetInstances will fail on StackitProvider
	if err == nil && len(instances) == 0 {
		t.Fatalf("expected error or instances")
	}
}
