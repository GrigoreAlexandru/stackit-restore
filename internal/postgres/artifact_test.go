package postgres

import (
	"os"
	"testing"
)

func TestArtifactManager_NewAndWriteMetadata(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dump-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mgr := NewArtifactManager(tempDir)
	artifact := mgr.NewDumpArtifact("inst-001", "Production", "app_prod", DumpModeStandard)

	if artifact.InstanceID != "inst-001" || artifact.DatabaseName != "app_prod" {
		t.Errorf("unexpected artifact fields: %+v", artifact)
	}

	if err := mgr.WriteMetadata(artifact); err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	metaFile := artifact.Path + ".json"
	if _, err := os.Stat(metaFile); err != nil {
		t.Errorf("expected metadata file to exist: %v", err)
	}

	readArtifact, err := mgr.ReadDumpArtifact(artifact.Name)
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}

	if readArtifact.InstanceID != "inst-001" || readArtifact.DatabaseName != "app_prod" {
		t.Errorf("unexpected read artifact fields: %+v", readArtifact)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"prod/db", "prod-db"},
		{"  db name  ", "db-name"},
		{"", "unknown"},
	}

	for _, tc := range cases {
		actual := SanitizeFileName(tc.input)
		if actual != tc.expected {
			t.Errorf("SanitizeFileName(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}
