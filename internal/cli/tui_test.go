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
