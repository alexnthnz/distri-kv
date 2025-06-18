package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestStorageEngine(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "distri-kv-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create logger for testing
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests

	// Create storage engine
	engine, err := NewStorageEngine(tempDir, 10, logger)
	if err != nil {
		t.Fatalf("Failed to create storage engine: %v", err)
	}
	defer engine.Close()

	t.Run("Basic Put and Get", func(t *testing.T) {
		key := "test_key"
		value := []byte("test_value")

		// Put value
		err := engine.Put(key, value)
		if err != nil {
			t.Errorf("Put failed: %v", err)
		}

		// Get value
		retrieved, exists, err := engine.Get(key)
		if err != nil {
			t.Errorf("Get failed: %v", err)
		}
		if !exists {
			t.Errorf("Key should exist")
		}
		if string(retrieved) != string(value) {
			t.Errorf("Retrieved value doesn't match. Expected: %s, Got: %s", value, retrieved)
		}
	})

	t.Run("Non-existent Key", func(t *testing.T) {
		_, exists, err := engine.Get("non_existent_key")
		if err != nil {
			t.Errorf("Get failed: %v", err)
		}
		if exists {
			t.Errorf("Non-existent key should not exist")
		}
	})

	t.Run("Delete Key", func(t *testing.T) {
		key := "delete_test"
		value := []byte("delete_value")

		// Put value
		err := engine.Put(key, value)
		if err != nil {
			t.Errorf("Put failed: %v", err)
		}

		// Verify it exists
		_, exists, err := engine.Get(key)
		if err != nil {
			t.Errorf("Get failed: %v", err)
		}
		if !exists {
			t.Errorf("Key should exist before deletion")
		}

		// Delete key
		err = engine.Delete(key)
		if err != nil {
			t.Errorf("Delete failed: %v", err)
		}

		// Verify it's gone
		_, exists, err = engine.Get(key)
		if err != nil {
			t.Errorf("Get failed: %v", err)
		}
		if exists {
			t.Errorf("Key should not exist after deletion")
		}
	})

	t.Run("Large Value Compression", func(t *testing.T) {
		key := "large_key"
		// Create a large value (> 1KB) to trigger compression
		largeValue := make([]byte, 2048)
		for i := range largeValue {
			largeValue[i] = byte(i % 256)
		}

		// Put large value
		err := engine.Put(key, largeValue)
		if err != nil {
			t.Errorf("Put failed: %v", err)
		}

		// Get large value
		retrieved, exists, err := engine.Get(key)
		if err != nil {
			t.Errorf("Get failed: %v", err)
		}
		if !exists {
			t.Errorf("Large key should exist")
		}
		if len(retrieved) != len(largeValue) {
			t.Errorf("Retrieved large value length doesn't match. Expected: %d, Got: %d", len(largeValue), len(retrieved))
		}

		// Verify content is identical
		for i, b := range retrieved {
			if b != largeValue[i] {
				t.Errorf("Large value content mismatch at position %d. Expected: %d, Got: %d", i, largeValue[i], b)
				break
			}
		}
	})

	t.Run("Multiple Keys", func(t *testing.T) {
		keys := []string{"key1", "key2", "key3", "key4", "key5"}
		values := []string{"value1", "value2", "value3", "value4", "value5"}

		// Store multiple keys
		for i, key := range keys {
			err := engine.Put(key, []byte(values[i]))
			if err != nil {
				t.Errorf("Put failed for key %s: %v", key, err)
			}
		}

		// Retrieve all keys
		for i, key := range keys {
			retrieved, exists, err := engine.Get(key)
			if err != nil {
				t.Errorf("Get failed for key %s: %v", key, err)
			}
			if !exists {
				t.Errorf("Key %s should exist", key)
			}
			if string(retrieved) != values[i] {
				t.Errorf("Value mismatch for key %s. Expected: %s, Got: %s", key, values[i], retrieved)
			}
		}
	})

	t.Run("Statistics", func(t *testing.T) {
		stats := engine.Stats()
		if stats == nil {
			t.Errorf("Stats should not be nil")
		}

		// Check that stats contain expected fields
		expectedFields := []string{"memory_entries", "disk_entries", "total_entries"}
		for _, field := range expectedFields {
			if _, exists := stats[field]; !exists {
				t.Errorf("Stats should contain field: %s", field)
			}
		}
	})

	t.Run("Memory Eviction", func(t *testing.T) {
		// Create a new engine with very small memory limit to force eviction
		smallEngine, err := NewStorageEngine(filepath.Join(tempDir, "small"), 2, logger)
		if err != nil {
			t.Fatalf("Failed to create small storage engine: %v", err)
		}
		defer smallEngine.Close()

		// Store more items than the memory limit
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("evict_key_%d", i)
			value := []byte(fmt.Sprintf("evict_value_%d", i))
			err := smallEngine.Put(key, value)
			if err != nil {
				t.Errorf("Put failed for key %s: %v", key, err)
			}

			// Add some delay to ensure different timestamps
			time.Sleep(time.Millisecond)
		}

		// All keys should still be retrievable (some from memory, some from disk)
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("evict_key_%d", i)
			expectedValue := fmt.Sprintf("evict_value_%d", i)

			retrieved, exists, err := smallEngine.Get(key)
			if err != nil {
				t.Errorf("Get failed for key %s: %v", key, err)
			}
			if !exists {
				t.Errorf("Key %s should exist even after eviction", key)
			}
			if string(retrieved) != expectedValue {
				t.Errorf("Value mismatch for key %s. Expected: %s, Got: %s", key, expectedValue, retrieved)
			}
		}
	})
}

func TestStorageEnginePersistence(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "distri-kv-persist-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create storage engine and store some data
	engine1, err := NewStorageEngine(tempDir, 10, logger)
	if err != nil {
		t.Fatalf("Failed to create storage engine: %v", err)
	}

	testData := map[string]string{
		"persistent_key1": "persistent_value1",
		"persistent_key2": "persistent_value2",
		"persistent_key3": "persistent_value3",
	}

	// Store test data
	for key, value := range testData {
		err := engine1.Put(key, []byte(value))
		if err != nil {
			t.Errorf("Put failed for key %s: %v", key, err)
		}
	}

	// Close the engine
	engine1.Close()

	// Create a new engine with the same directory
	engine2, err := NewStorageEngine(tempDir, 10, logger)
	if err != nil {
		t.Fatalf("Failed to create second storage engine: %v", err)
	}
	defer engine2.Close()

	// Verify that data persisted
	for key, expectedValue := range testData {
		retrieved, exists, err := engine2.Get(key)
		if err != nil {
			t.Errorf("Get failed for key %s: %v", key, err)
		}
		if !exists {
			t.Errorf("Persistent key %s should exist", key)
		}
		if string(retrieved) != expectedValue {
			t.Errorf("Persistent value mismatch for key %s. Expected: %s, Got: %s", key, expectedValue, retrieved)
		}
	}
}

func BenchmarkStorageEngine(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "distri-kv-bench-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Minimize logging in benchmarks

	engine, err := NewStorageEngine(tempDir, 1000, logger)
	if err != nil {
		b.Fatalf("Failed to create storage engine: %v", err)
	}
	defer engine.Close()

	b.Run("Put", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("bench_key_%d", i)
			value := []byte(fmt.Sprintf("bench_value_%d", i))
			engine.Put(key, value)
		}
	})

	// Pre-populate for Get benchmark
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("get_bench_key_%d", i)
		value := []byte(fmt.Sprintf("get_bench_value_%d", i))
		engine.Put(key, value)
	}

	b.Run("Get", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("get_bench_key_%d", i%1000)
			engine.Get(key)
		}
	})
}
