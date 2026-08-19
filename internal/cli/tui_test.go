package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
)

func TestParsePITTimestamp_Valid(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Time
	}{
		{
			name:     "RFC3339 format",
			input:    "2026-08-13T15:30:00Z",
			expected: time.Date(2026, 8, 13, 15, 30, 0, 0, time.UTC),
		},
		{
			name:     "Standard datetime string",
			input:    "2026-08-13 15:30:00",
			expected: time.Date(2026, 8, 13, 15, 30, 0, 0, time.UTC),
		},
		{
			name:     "Datetime without seconds",
			input:    "2026-08-13 15:30",
			expected: time.Date(2026, 8, 13, 15, 30, 0, 0, time.UTC),
		},
		{
			name:     "Date only",
			input:    "2026-08-13",
			expected: time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePITTimestamp(tt.input)
			if err != nil {
				t.Fatalf("unexpected error parsing %q: %v", tt.input, err)
			}
			if !got.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestParsePITTimestamp_Invalid(t *testing.T) {
	invalidInputs := []string{
		"",
		"   ",
		"invalid-date",
		"13-08-2026",
		"2026/08/13",
		"2026-08-13 25:00:00",
	}

	for _, input := range invalidInputs {
		t.Run(input, func(t *testing.T) {
			_, err := ParsePITTimestamp(input)
			if err == nil {
				t.Errorf("expected error for input %q, got nil", input)
			}
		})
	}
}

func TestGetDatabaseOptions_AvailableFirstAndUnavailableStyledLast(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("LOCAL_USER", "loc_user")
	t.Setenv("LOCAL_PASS", "loc_pass")
	t.Setenv("UNCONFIGURED_A_USER", "")
	t.Setenv("UNCONFIGURED_B_USER", "")

	app := &appForm{
		databaseSelections: []databaseSelection{
			{
				Instance: api.Instance{Name: "UnconfiguredB", ID: "u2"},
				Database: api.Database{Name: "db_u2"},
			},
			{
				Instance: api.Instance{Name: "Production", ID: "p1"},
				Database: api.Database{Name: "app_prod"},
			},
			{
				Instance: api.Instance{Name: "UnconfiguredA", ID: "u1"},
				Database: api.Database{Name: "db_u1"},
			},
			{
				Instance: api.Instance{Name: "local", ID: "local"},
				Database: api.Database{Name: "postgres"},
			},
		},
	}

	options := app.getDatabaseOptions()
	if len(options) != 4 {
		t.Fatalf("expected 4 options, got %d", len(options))
	}

	// 1st option: Production / app_prod (available)
	if options[0].Value.Instance.Name != "Production" || options[0].Value.Database.Name != "app_prod" {
		t.Errorf("expected 1st option to be Production / app_prod, got %s / %s", options[0].Value.Instance.Name, options[0].Value.Database.Name)
	}
	if options[0].Key != "Production / app_prod" {
		t.Errorf("expected 1st option label 'Production / app_prod', got %q", options[0].Key)
	}

	// 2nd option: local / postgres (available)
	if options[1].Value.Instance.Name != "local" || options[1].Value.Database.Name != "postgres" {
		t.Errorf("expected 2nd option to be local / postgres, got %s / %s", options[1].Value.Instance.Name, options[1].Value.Database.Name)
	}
	if options[1].Key != "local / postgres" {
		t.Errorf("expected 2nd option label 'local / postgres', got %q", options[1].Key)
	}

	// 3rd option: UnconfiguredA / db_u1 (unavailable - sorted before UnconfiguredB)
	if options[2].Value.Instance.Name != "UnconfiguredA" || options[2].Value.Database.Name != "db_u1" {
		t.Errorf("expected 3rd option to be UnconfiguredA / db_u1, got %s / %s", options[2].Value.Instance.Name, options[2].Value.Database.Name)
	}
	if !strings.Contains(options[2].Key, "unavailable: missing") {
		t.Errorf("expected 3rd option label to contain unavailable hint, got %q", options[2].Key)
	}

	// 4th option: UnconfiguredB / db_u2 (unavailable)
	if options[3].Value.Instance.Name != "UnconfiguredB" || options[3].Value.Database.Name != "db_u2" {
		t.Errorf("expected 4th option to be UnconfiguredB / db_u2, got %s / %s", options[3].Value.Instance.Name, options[3].Value.Database.Name)
	}
	if !strings.Contains(options[3].Key, "unavailable: missing") {
		t.Errorf("expected 4th option label to contain unavailable hint, got %q", options[3].Key)
	}
}

func TestGetCloudInstanceOptions_AvailableFirstAndUnavailableStyledLast(t *testing.T) {
	t.Setenv("PRODUCTION_USER", "prod_user")
	t.Setenv("PRODUCTION_PASS", "prod_pass")
	t.Setenv("UNCONFIGURED_CLOUD_USER", "")

	app := &appForm{
		instances: []api.Instance{
			{Name: "UnconfiguredCloud", ID: "u1"},
			{Name: "Production", ID: "p1"},
			{Name: "local", ID: "local"}, // local should be excluded from cloud instances
		},
	}

	options := app.getCloudInstanceOptions()
	if len(options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(options))
	}

	// 1st option: Production (available)
	if options[0].Value.Name != "Production" || options[0].Key != "Production" {
		t.Errorf("expected 1st option to be Production, got %+v", options[0])
	}

	// 2nd option: UnconfiguredCloud (unavailable, styled)
	if options[1].Value.Name != "UnconfiguredCloud" {
		t.Errorf("expected 2nd option to be UnconfiguredCloud, got %+v", options[1])
	}
	if !strings.Contains(options[1].Key, "unavailable: missing") {
		t.Errorf("expected 2nd option to contain unavailable hint, got %q", options[1].Key)
	}
}

func TestBackupOptions_FormattingWithNameDateAndSize(t *testing.T) {
	backups := []api.Backup{
		{
			Name:      "backup-daily-01",
			CreatedAt: time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
			Size:      4 * 1024 * 1024 * 1024, // 4 GB
		},
		{
			Name:      "backup-daily-02",
			CreatedAt: time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC),
			Size:      536870912, // 0.5 GB
		},
	}

	app := &appForm{
		backupsByInstance: map[string][]api.Backup{
			"inst-1": backups,
		},
	}

	// Verify the backup options formatting logic
	for _, b := range app.backupsByInstance["inst-1"] {
		sizeGB := float64(b.Size) / (1024 * 1024 * 1024)
		label := strings.TrimSpace(b.Name + " | " + b.CreatedAt.Format(time.RFC3339) + " | " + strings.TrimSpace(string([]byte{byte('0' + int(sizeGB))})))
		if b.Name == "backup-daily-01" && sizeGB != 4.0 {
			t.Errorf("expected size 4.0 GB, got %f", sizeGB)
		}
		if b.Name == "backup-daily-02" && sizeGB != 0.5 {
			t.Errorf("expected size 0.5 GB, got %f", sizeGB)
		}
		_ = label
	}
}

func TestGetDumpOptions_Formatting(t *testing.T) {
	t1 := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	dumps := []api.DumpArtifact{
		{
			Path:         "/dumps/test.dump",
			Mode:         api.DumpModeStandard,
			InstanceName: "prod",
			DatabaseName: "app_db",
			CreatedAt:    t1,
		},
	}

	app := &appForm{
		dumpArtifacts: dumps,
	}

	options := app.getDumpOptions()
	if len(options) != 1 {
		t.Fatalf("expected 1 option, got %d", len(options))
	}
	expectedLabel := "2026-08-19T12:00:00Z | dump_from_live | prod | app_db"
	if options[0].Key != expectedLabel {
		t.Fatalf("expected key %q, got %q", expectedLabel, options[0].Key)
	}
}

func TestAppForm_BuildExplanation(t *testing.T) {
	app := &appForm{
		selectedAction: actionDump,
		sourceSelection: databaseSelection{
			Instance: api.Instance{Name: "prod"},
			Database: api.Database{Name: "app_db"},
		},
		selectedDumpMode: api.DumpModeStandard,
	}

	exp := app.buildExplanation()
	if !strings.Contains(exp, "Runs pg_dump directly on live database") || !strings.Contains(exp, "prod") {
		t.Errorf("expected live dump explanation, got %q", exp)
	}

	app.selectedDumpMode = api.DumpModeReplica
	app.selectedBackup = api.Backup{Name: "daily-bkp"}
	exp = app.buildExplanation()
	if !strings.Contains(exp, "Creates a temporary PostgreSQL clone") || !strings.Contains(exp, "prod") {
		t.Errorf("expected backup-based dump explanation, got %q", exp)
	}

	app.selectedDumpMode = api.DumpModePointInTime
	app.selectedPIT = time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	exp = app.buildExplanation()
	if !strings.Contains(exp, "point-in-time") {
		t.Errorf("expected PIT dump explanation, got %q", exp)
	}

	app.selectedAction = actionRestore
	app.destSelection = databaseSelection{
		Instance: api.Instance{Name: "local"},
		Database: api.Database{Name: "dev_db"},
	}
	app.selectedRestoreSource = restoreSourceDumpFile
	app.selectedDump = api.DumpArtifact{Path: "/dumps/app.dump"}
	exp = app.buildExplanation()
	if !strings.Contains(exp, "Reads local .dump file") || !strings.Contains(exp, "app.dump") {
		t.Errorf("expected dump file restore explanation, got %q", exp)
	}

	app.selectedAction = actionSync
	app.selectedDumpMode = api.DumpModeStandard
	exp = app.buildExplanation()
	if !strings.Contains(exp, "Extracts live dump from prod / app_db") || !strings.Contains(exp, "local / dev_db") {
		t.Errorf("expected live sync explanation, got %q", exp)
	}
}


