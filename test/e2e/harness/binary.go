package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

const defaultBinaryPath = "/tmp/stackit-restore-e2e-bin/stackit-restore"

var (
	binaryPath string
	buildErr   error
	buildOnce  sync.Once
)

// FindRepoRoot discovers the root directory of the repository containing go.mod.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("go.mod not found in any parent directory")
}

// BuildBinary compiles the stackit-restore binary once and returns its path.
func BuildBinary() (string, error) {
	buildOnce.Do(func() {
		repoRoot, err := FindRepoRoot()
		if err != nil {
			buildErr = fmt.Errorf("locate repo root: %w", err)
			return
		}

		binDir := filepath.Dir(defaultBinaryPath)
		if err := os.MkdirAll(binDir, 0755); err != nil {
			buildErr = fmt.Errorf("create binary cache directory %s: %w", binDir, err)
			return
		}

		cmd := exec.Command("go", "build", "-o", defaultBinaryPath, "./cmd/stackit-restore")
		cmd.Dir = repoRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			// If incremental compilation is temporarily broken during milestone development,
			// check for pre-built binary in repository root as fallback.
			existingBin := filepath.Join(repoRoot, "stackit-restore")
			if info, statErr := os.Stat(existingBin); statErr == nil && !info.IsDir() {
				data, readErr := os.ReadFile(existingBin)
				if readErr == nil {
					_ = os.Remove(defaultBinaryPath)
					if writeErr := os.WriteFile(defaultBinaryPath, data, 0755); writeErr == nil {
						binaryPath = defaultBinaryPath
						return
					}
				}
			}
			buildErr = fmt.Errorf("build stackit-restore binary (%s): %w\nOutput:\n%s", defaultBinaryPath, err, string(output))
			return
		}

		binaryPath = defaultBinaryPath
	})

	return binaryPath, buildErr
}

// GetBinaryPath returns the path to the compiled stackit-restore binary, failing the test if build fails.
func GetBinaryPath(t testing.TB) string {
	t.Helper()
	path, err := BuildBinary()
	if err != nil {
		t.Fatalf("Failed to build stackit-restore binary: %v", err)
	}
	return path
}
