package cli

import (
	"testing"
	"time"
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
