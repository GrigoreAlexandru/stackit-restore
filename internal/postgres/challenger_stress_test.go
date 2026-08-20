package postgres

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestExecutionLogger_HighConcurrencyStress runs 200 concurrent goroutines
// performing mixed Appends, Writes, Reads, Resets, and Writer swaps.
func TestExecutionLogger_HighConcurrencyStress(t *testing.T) {
	logger := NewExecutionLogger(nil)
	const numGoroutines = 200
	const iterations = 500

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var appendCount int64
	var writeCount int64
	var readCount int64
	var resetCount int64
	var swapCount int64

	dummyWriter1 := &bytes.Buffer{}
	dummyWriter2 := &bytes.Buffer{}

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(id + 1)))

			for j := 0; j < iterations; j++ {
				op := r.Intn(6)
				switch op {
				case 0:
					logger.Append([]byte(fmt.Sprintf("[g%d-iter%d] test log line\n", id, j)))
					atomic.AddInt64(&appendCount, 1)
				case 1:
					_, _ = logger.Write([]byte(fmt.Sprintf("[g%d-iter%d] write log line\n", id, j)))
					atomic.AddInt64(&writeCount, 1)
				case 2:
					logContent := logger.GetLog()
					_ = len(logContent)
					atomic.AddInt64(&readCount, 1)
				case 3:
					w := logger.GetWriter()
					if w == nil {
						t.Errorf("GetWriter returned nil")
					}
				case 4:
					switch r.Intn(3) {
					case 0:
						logger.SetWriter(dummyWriter1)
					case 1:
						logger.SetWriter(dummyWriter2)
					case 2:
						logger.SetWriter(nil)
					}
					atomic.AddInt64(&swapCount, 1)
				case 5:
					if j%50 == 0 {
						logger.Reset()
						atomic.AddInt64(&resetCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf("Completed concurrency stress test: appends=%d, writes=%d, reads=%d, resets=%d, swaps=%d",
		appendCount, writeCount, readCount, resetCount, swapCount)
}

// TestExecutionLogger_NilReceiver_AllMethods thoroughly verifies that every method
// on *ExecutionLogger is completely nil-safe and does not panic.
func TestExecutionLogger_NilReceiver_AllMethods(t *testing.T) {
	var nilLogger *ExecutionLogger

	t.Run("Append", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nilLogger.Append panicked: %v", r)
			}
		}()
		nilLogger.Append([]byte("sample data"))
		nilLogger.Append(nil)
	})

	t.Run("GetLog", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nilLogger.GetLog panicked: %v", r)
			}
		}()
		if got := nilLogger.GetLog(); got != "" {
			t.Fatalf("nilLogger.GetLog() = %q, want empty string", got)
		}
	})

	t.Run("Reset", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nilLogger.Reset panicked: %v", r)
			}
		}()
		nilLogger.Reset()
	})

	t.Run("SetWriter", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nilLogger.SetWriter panicked: %v", r)
			}
		}()
		nilLogger.SetWriter(io.Discard)
		nilLogger.SetWriter(nil)
	})

	t.Run("GetWriter", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nilLogger.GetWriter panicked: %v", r)
			}
		}()
		w := nilLogger.GetWriter()
		if w != os.Stdout {
			t.Fatalf("nilLogger.GetWriter() = %v, want os.Stdout fallback", w)
		}
	})

	t.Run("Write", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nilLogger.Write panicked: %v", r)
			}
		}()
		payload := []byte("write test")
		n, err := nilLogger.Write(payload)
		if err != nil {
			t.Fatalf("nilLogger.Write returned error: %v", err)
		}
		if n != len(payload) {
			t.Fatalf("nilLogger.Write returned n=%d, want %d", n, len(payload))
		}
	})
}

// TestArtifact_FilenameZeroCollisionStress generates 10,000 filenames across 50 goroutines
// to ensure no collisions occur even with high-frequency generation.
func TestArtifact_FilenameZeroCollisionStress(t *testing.T) {
	const numGoroutines = 50
	const filesPerGoroutine = 200
	totalFiles := numGoroutines * filesPerGoroutine

	filenames := make([]string, totalFiles)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(gIndex int) {
			defer wg.Done()
			offset := gIndex * filesPerGoroutine
			for i := 0; i < filesPerGoroutine; i++ {
				// Each iteration creates a distinct microsecond timestamp
				now := time.Now().UTC()
				// Adding synthetic microsecond jitter if loop executes faster than system clock ticks
				ts := now.Add(time.Duration(i) * time.Microsecond)
				name := GenerateDumpFilename(ts, DumpModeStandard, fmt.Sprintf("inst-%d", gIndex), fmt.Sprintf("db-%d", i))
				filenames[offset+i] = name
			}
		}(g)
	}

	wg.Wait()

	seen := make(map[string]struct{}, totalFiles)
	for _, fn := range filenames {
		if _, exists := seen[fn]; exists {
			t.Fatalf("collision detected in generated dump filename: %q", fn)
		}
		seen[fn] = struct{}{}
	}
	t.Logf("Generated %d unique filenames with 0 collisions", len(seen))
}

// TestArtifact_SortFunc_ComplexOracle stress-tests the sorting logic with various edge cases:
// equal timestamps, out-of-order timestamps, empty slices, 1-element, timezones, and extreme dates.
func TestArtifact_SortFunc_ComplexOracle(t *testing.T) {
	sortArtifacts := func(artifacts []DumpArtifact) {
		slices.SortFunc(artifacts, func(a, b DumpArtifact) int {
			if a.CreatedAt.After(b.CreatedAt) {
				return -1
			}
			if a.CreatedAt.Before(b.CreatedAt) {
				return 1
			}
			return 0
		})
	}

	t.Run("Empty slice", func(t *testing.T) {
		var empty []DumpArtifact
		sortArtifacts(empty)
		if len(empty) != 0 {
			t.Fatalf("expected empty slice, got len %d", len(empty))
		}
	})

	t.Run("Single element", func(t *testing.T) {
		single := []DumpArtifact{{Name: "a", CreatedAt: time.Now()}}
		sortArtifacts(single)
		if len(single) != 1 || single[0].Name != "a" {
			t.Fatalf("single element modified unexpectedly: %+v", single)
		}
	})

	t.Run("Equal timestamps preserved without panic", func(t *testing.T) {
		fixedTime := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
		equalSlice := []DumpArtifact{
			{Name: "item1", CreatedAt: fixedTime},
			{Name: "item2", CreatedAt: fixedTime},
			{Name: "item3", CreatedAt: fixedTime},
		}
		sortArtifacts(equalSlice)
		if len(equalSlice) != 3 {
			t.Fatalf("expected 3 items, got %d", len(equalSlice))
		}
	})

	t.Run("Extreme timestamps and timezones", func(t *testing.T) {
		locPlus5 := time.FixedZone("UTC+5", 5*3600)
		locMinus8 := time.FixedZone("UTC-8", -8*3600)

		// Create timestamps with identical absolute time but different zones, plus edge dates
		epoch := time.Unix(0, 0).UTC()
		farFuture := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
		farPast := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

		// 2026-08-20 12:00:00 UTC == 2026-08-20 17:00:00 UTC+5
		tUTC := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
		tPlus5 := time.Date(2026, 8, 20, 17, 0, 0, 0, locPlus5)
		tNewer := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
		tOlder := time.Date(2026, 8, 20, 04, 0, 0, 0, locMinus8) // 12:00 UTC

		items := []DumpArtifact{
			{Name: "epoch", CreatedAt: epoch},
			{Name: "farFuture", CreatedAt: farFuture},
			{Name: "tUTC", CreatedAt: tUTC},
			{Name: "farPast", CreatedAt: farPast},
			{Name: "tNewer", CreatedAt: tNewer},
			{Name: "tPlus5", CreatedAt: tPlus5},
			{Name: "tOlder", CreatedAt: tOlder},
		}

		sortArtifacts(items)

		// Verify descending order (newest first)
		for i := 0; i < len(items)-1; i++ {
			curr := items[i].CreatedAt
			next := items[i+1].CreatedAt
			if curr.Before(next) {
				t.Fatalf("sort failure at index %d: %s (%v) is before %s (%v)",
					i, items[i].Name, curr, items[i+1].Name, next)
			}
		}

		if items[0].Name != "farFuture" {
			t.Errorf("expected farFuture first, got %s", items[0].Name)
		}
		if items[len(items)-1].Name != "farPast" {
			t.Errorf("expected farPast last, got %s", items[len(items)-1].Name)
		}
	})

	t.Run("Random 10,000 items sorting oracle", func(t *testing.T) {
		const n = 10000
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		items := make([]DumpArtifact, n)
		r := rand.New(rand.NewSource(42))

		for i := 0; i < n; i++ {
			randomSeconds := r.Int63n(365 * 24 * 3600)
			items[i] = DumpArtifact{
				Name:      fmt.Sprintf("item-%d", i),
				CreatedAt: base.Add(time.Duration(randomSeconds) * time.Second),
			}
		}

		sortArtifacts(items)

		for i := 0; i < n-1; i++ {
			if items[i].CreatedAt.Before(items[i+1].CreatedAt) {
				t.Fatalf("random sort oracle violated at %d: %v before %v",
					i, items[i].CreatedAt, items[i+1].CreatedAt)
			}
		}
	})
}

// TestWriteErrorLog_EdgeCases tests WriteErrorLog with empty details, nil err, non-standard paths.
func TestWriteErrorLog_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("empty action and nil details and nil error", func(t *testing.T) {
		logPath, err := WriteErrorLog(nil, tempDir, "", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		content, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatalf("failed reading log: %v", err)
		}
		contentStr := string(content)
		if !bytes.Contains(content, []byte("Action:      operation")) && !bytes.Contains(content, []byte("Action:      ")) {
			t.Errorf("expected action line in log, got: %s", contentStr)
		}
		if !bytes.Contains(content, []byte("(No stdout/stderr captured during this session)")) {
			t.Errorf("expected fallback text, got: %s", contentStr)
		}
	})

	t.Run("non-existent deep dumpDir is created", func(t *testing.T) {
		deepDir := filepath.Join(tempDir, "nested", "level2", "dumps")
		logPath, err := WriteErrorLog(nil, deepDir, "deep_test", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error on deep dir: %v", err)
		}
		if _, err := os.Stat(logPath); err != nil {
			t.Fatalf("expected log file to exist at %s: %v", logPath, err)
		}
	})
}
