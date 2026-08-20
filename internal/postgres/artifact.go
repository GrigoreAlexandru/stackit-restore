package postgres

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type DumpMode string

const (
	DumpModeStandard    DumpMode = "dump_from_live"
	DumpModeReplica     DumpMode = "dump_from_replica"
	DumpModePointInTime DumpMode = "dump_from_pit"
)

type DumpArtifact struct {
	Name         string
	Path         string
	Mode         DumpMode
	InstanceName string
	InstanceID   string
	DatabaseName string
	CreatedAt    time.Time
}

type dumpArtifactMetadata struct {
	Mode         DumpMode  `json:"mode"`
	InstanceName string    `json:"instanceName"`
	InstanceID   string    `json:"instanceId"`
	DatabaseName string    `json:"databaseName"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ArtifactManager struct {
	dumpDir string
}

func NewArtifactManager(dumpDir string) *ArtifactManager {
	return &ArtifactManager{dumpDir: dumpDir}
}

func (m *ArtifactManager) ListDumpArtifacts() ([]DumpArtifact, error) {
	entries, err := os.ReadDir(m.dumpDir)
	if err != nil {
		return nil, fmt.Errorf("read dump directory %q: %w", m.dumpDir, err)
	}

	artifacts := make([]DumpArtifact, 0, len(entries))
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".dump" && ext != ".sql") {
			continue
		}

		artifact, err := m.ReadDumpArtifact(entry.Name())
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}

	slices.SortFunc(artifacts, func(a, b DumpArtifact) int {
		if a.CreatedAt.After(b.CreatedAt) {
			return -1
		}
		if a.CreatedAt.Before(b.CreatedAt) {
			return 1
		}
		return 0
	})

	return artifacts, nil
}

func GenerateDumpFilename(
	timestamp time.Time,
	mode DumpMode,
	instanceID string,
	databaseName string,
) string {
	return fmt.Sprintf(
		"%s__%s__%s__%s.dump",
		timestamp.UTC().Format("20060102T150405.000000Z"),
		mode,
		SanitizeFileName(instanceID),
		SanitizeFileName(databaseName),
	)
}

func (m *ArtifactManager) NewDumpArtifact(
	instanceID string,
	instanceName string,
	databaseName string,
	mode DumpMode,
) DumpArtifact {
	timestamp := time.Now().UTC()
	filename := GenerateDumpFilename(timestamp, mode, instanceID, databaseName)
	path := filepath.Join(m.dumpDir, filename)

	return DumpArtifact{
		Name:         filename,
		Path:         path,
		Mode:         mode,
		InstanceName: instanceName,
		InstanceID:   instanceID,
		DatabaseName: databaseName,
		CreatedAt:    timestamp,
	}
}

func (m *ArtifactManager) ReadDumpArtifact(fileName string) (DumpArtifact, error) {
	path := filepath.Join(m.dumpDir, fileName)
	metaPath := path + ".json"

	metadataBytes, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m.readDumpArtifactFromFileInfo(fileName)
		}
		return DumpArtifact{}, fmt.Errorf("read dump metadata for %q: %w", fileName, err)
	}

	var meta dumpArtifactMetadata
	if err := json.Unmarshal(metadataBytes, &meta); err != nil {
		return DumpArtifact{}, fmt.Errorf("parse dump metadata for %q: %w", fileName, err)
	}

	return DumpArtifact{
		Name:         fileName,
		Path:         path,
		Mode:         meta.Mode,
		InstanceName: meta.InstanceName,
		InstanceID:   meta.InstanceID,
		DatabaseName: meta.DatabaseName,
		CreatedAt:    meta.CreatedAt,
	}, nil
}

func (m *ArtifactManager) readDumpArtifactFromFileInfo(fileName string) (DumpArtifact, error) {
	path := filepath.Join(m.dumpDir, fileName)
	info, err := os.Stat(path)
	if err != nil {
		return DumpArtifact{}, fmt.Errorf("stat dump file %q: %w", fileName, err)
	}

	mode := DumpModeStandard
	switch {
	case strings.Contains(fileName, string(DumpModePointInTime)):
		mode = DumpModePointInTime
	case strings.Contains(fileName, string(DumpModeReplica)):
		mode = DumpModeReplica
	}

	return DumpArtifact{
		Name:      fileName,
		Path:      path,
		Mode:      mode,
		CreatedAt: info.ModTime().UTC(),
	}, nil
}

func (m *ArtifactManager) WriteMetadata(artifact DumpArtifact) error {
	metaPath := artifact.Path + ".json"
	meta := dumpArtifactMetadata{
		Mode:         artifact.Mode,
		InstanceName: artifact.InstanceName,
		InstanceID:   artifact.InstanceID,
		DatabaseName: artifact.DatabaseName,
		CreatedAt:    artifact.CreatedAt,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal dump metadata for %q: %w", artifact.Name, err)
	}

	if err := os.WriteFile(metaPath, data, 0o600); err != nil {
		return fmt.Errorf("write dump metadata for %q: %w", artifact.Name, err)
	}
	return nil
}

func SanitizeFileName(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.ReplaceAll(normalized, "/", "-")
	normalized = strings.ReplaceAll(normalized, "\\", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	if normalized == "" {
		return "unknown"
	}
	return normalized
}
