package postgres

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
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

func TestGenerateDumpFilename(t *testing.T) {
	ts := time.Date(2026, 8, 20, 14, 30, 15, 123456000, time.UTC)
	filename := GenerateDumpFilename(ts, DumpModeStandard, "inst-001", "mydb")

	expected := "20260820T143015.123456Z__dump_from_live__inst-001__mydb.dump"
	if filename != expected {
		t.Fatalf("GenerateDumpFilename() = %q, want %q", filename, expected)
	}
}

func TestArtifactManager_MicrosecondFilenamePrecision(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewArtifactManager(tempDir)

	// Rapid back-to-back generation
	a1 := mgr.NewDumpArtifact("inst-1", "Instance 1", "db1", DumpModeStandard)
	a2 := mgr.NewDumpArtifact("inst-1", "Instance 1", "db1", DumpModeStandard)

	// Filename should match the microsecond regex: YYYYMMDDTHHMMSS.ffffffZ__...
	microsecondPattern := regexp.MustCompile(`^\d{8}T\d{6}\.\d{6}Z__`)
	if !microsecondPattern.MatchString(a1.Name) {
		t.Errorf("expected a1 name %q to have microsecond format", a1.Name)
	}
	if !microsecondPattern.MatchString(a2.Name) {
		t.Errorf("expected a2 name %q to have microsecond format", a2.Name)
	}

	// Filenames must not collide even if created rapidly
	// (Unless clock has < 1 microsecond resolution, in which case small sleep guarantees uniqueness)
	if a1.Name == a2.Name {
		// Verify with a tiny microsecond delay if immediate was identical due to sub-micro resolution
		time.Sleep(1 * time.Microsecond)
		a3 := mgr.NewDumpArtifact("inst-1", "Instance 1", "db1", DumpModeStandard)
		if a1.Name == a3.Name {
			t.Errorf("expected distinct filenames for dumps created at different microseconds: %q vs %q", a1.Name, a3.Name)
		}
	}
}

func TestArtifactManager_ListDumpArtifacts_Sorting(t *testing.T) {
	tempDir := t.TempDir()
	mgr := NewArtifactManager(tempDir)

	// Create 3 artifacts with distinct timestamps
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)

	// Write mock dump files & metadata in unsorted order (t2, t1, t3)
	for i, ts := range []time.Time{t2, t1, t3} {
		fn := GenerateDumpFilename(ts, DumpModeStandard, fmt.Sprintf("inst-%d", i), "db")
		filePath := filepath.Join(tempDir, fn)
		if err := os.WriteFile(filePath, []byte("dumpcontent"), 0644); err != nil {
			t.Fatalf("failed to create dump file: %v", err)
		}

		art := DumpArtifact{
			Name:         fn,
			Path:         filePath,
			Mode:         DumpModeStandard,
			InstanceName: fmt.Sprintf("Instance %d", i),
			InstanceID:   fmt.Sprintf("inst-%d", i),
			DatabaseName: "db",
			CreatedAt:    ts,
		}
		if err := mgr.WriteMetadata(art); err != nil {
			t.Fatalf("failed to write metadata: %v", err)
		}
	}

	list, err := mgr.ListDumpArtifacts()
	if err != nil {
		t.Fatalf("ListDumpArtifacts returned error: %v", err)
	}

	if len(list) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(list))
	}

	// Should be descending: t3 (newest), t2, t1 (oldest)
	if !list[0].CreatedAt.Equal(t3) {
		t.Errorf("expected first artifact to have CreatedAt %v, got %v", t3, list[0].CreatedAt)
	}
	if !list[1].CreatedAt.Equal(t2) {
		t.Errorf("expected second artifact to have CreatedAt %v, got %v", t2, list[1].CreatedAt)
	}
	if !list[2].CreatedAt.Equal(t1) {
		t.Errorf("expected third artifact to have CreatedAt %v, got %v", t1, list[2].CreatedAt)
	}
}
