package storage

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/sirupsen/logrus"
)

// StorageEntry represents a single key-value entry with metadata
type StorageEntry struct {
	Key         string
	Value       []byte
	Timestamp   time.Time
	AccessCount int64
	Compressed  bool
}

// StorageEngine handles persistent storage with compression and hot/cold data management
type StorageEngine struct {
	dataDir      string
	memCache     map[string]*StorageEntry
	diskIndex    map[string]string // key -> file path
	mu           sync.RWMutex
	compressor   *zstd.Encoder
	decompressor *zstd.Decoder
	maxMemItems  int
	logger       *logrus.Logger
}

// NewStorageEngine creates a new storage engine
func NewStorageEngine(dataDir string, maxMemItems int, logger *logrus.Logger) (*StorageEngine, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	compressor, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create compressor: %w", err)
	}

	decompressor, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create decompressor: %w", err)
	}

	engine := &StorageEngine{
		dataDir:      dataDir,
		memCache:     make(map[string]*StorageEntry),
		diskIndex:    make(map[string]string),
		compressor:   compressor,
		decompressor: decompressor,
		maxMemItems:  maxMemItems,
		logger:       logger,
	}

	// Load existing data from disk
	if err := engine.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("failed to load existing data: %w", err)
	}

	return engine, nil
}

// Put stores a key-value pair
func (e *StorageEngine) Put(key string, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry := &StorageEntry{
		Key:         key,
		Value:       value,
		Timestamp:   time.Now(),
		AccessCount: 1,
		Compressed:  false,
	}

	// Compress large values
	if len(value) > 1024 { // Compress values larger than 1KB
		compressed := e.compressor.EncodeAll(value, make([]byte, 0, len(value)))
		if len(compressed) < len(value) {
			entry.Value = compressed
			entry.Compressed = true
		}
	}

	// Store in memory cache
	e.memCache[key] = entry

	// If memory cache is full, evict cold data to disk
	if len(e.memCache) > e.maxMemItems {
		e.evictColdData()
	}

	e.logger.Debugf("Stored key %s in memory cache", key)
	return nil
}

// Get retrieves a value by key
func (e *StorageEngine) Get(key string) ([]byte, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check memory cache first (hot data)
	if entry, exists := e.memCache[key]; exists {
		entry.AccessCount++
		entry.Timestamp = time.Now()

		value := entry.Value
		if entry.Compressed {
			decompressed, err := e.decompressor.DecodeAll(entry.Value, nil)
			if err != nil {
				return nil, false, fmt.Errorf("failed to decompress value: %w", err)
			}
			value = decompressed
		}

		e.logger.Debugf("Retrieved key %s from memory cache", key)
		return value, true, nil
	}

	// Check disk index (cold data)
	if filePath, exists := e.diskIndex[key]; exists {
		entry, err := e.loadFromDiskFile(filePath)
		if err != nil {
			return nil, false, fmt.Errorf("failed to load from disk: %w", err)
		}

		// Move back to memory cache as it's now hot
		entry.AccessCount++
		entry.Timestamp = time.Now()
		e.memCache[key] = entry

		// If memory cache is full, evict other cold data
		if len(e.memCache) > e.maxMemItems {
			e.evictColdData()
		}

		value := entry.Value
		if entry.Compressed {
			decompressed, err := e.decompressor.DecodeAll(entry.Value, nil)
			if err != nil {
				return nil, false, fmt.Errorf("failed to decompress value: %w", err)
			}
			value = decompressed
		}

		e.logger.Debugf("Retrieved key %s from disk and moved to memory", key)
		return value, true, nil
	}

	return nil, false, nil
}

// Delete removes a key-value pair
func (e *StorageEngine) Delete(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove from memory cache
	delete(e.memCache, key)

	// Remove from disk if exists
	if filePath, exists := e.diskIndex[key]; exists {
		os.Remove(filePath)
		delete(e.diskIndex, key)
	}

	e.logger.Debugf("Deleted key %s", key)
	return nil
}

// evictColdData moves least frequently used data from memory to disk
func (e *StorageEngine) evictColdData() {
	if len(e.memCache) <= e.maxMemItems {
		return
	}

	// Find entries to evict (least recently used with low access count)
	var coldEntries []*StorageEntry
	for _, entry := range e.memCache {
		coldEntries = append(coldEntries, entry)
	}

	// Sort by access count and timestamp
	// Simple heuristic: evict entries with low access count and old timestamp
	toEvict := len(e.memCache) - e.maxMemItems
	evicted := 0

	for _, entry := range coldEntries {
		if evicted >= toEvict {
			break
		}

		if entry.AccessCount < 5 && time.Since(entry.Timestamp) > 5*time.Minute {
			if err := e.saveToDisk(entry); err != nil {
				e.logger.Errorf("Failed to save entry to disk: %v", err)
				continue
			}

			delete(e.memCache, entry.Key)
			evicted++
			e.logger.Debugf("Evicted key %s to disk", entry.Key)
		}
	}
}

// saveToDisk saves an entry to disk
func (e *StorageEngine) saveToDisk(entry *StorageEntry) error {
	filename := fmt.Sprintf("%s.dat", entry.Key)
	filePath := filepath.Join(e.dataDir, filename)

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(entry); err != nil {
		return fmt.Errorf("failed to encode entry: %w", err)
	}

	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	e.diskIndex[entry.Key] = filePath
	return nil
}

// loadFromDiskFile loads an entry from a disk file
func (e *StorageEngine) loadFromDiskFile(filePath string) (*StorageEntry, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var entry StorageEntry
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&entry); err != nil {
		return nil, fmt.Errorf("failed to decode entry: %w", err)
	}

	return &entry, nil
}

// loadFromDisk loads existing data from disk on startup
func (e *StorageEngine) loadFromDisk() error {
	files, err := os.ReadDir(e.dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".dat" {
			continue
		}

		filePath := filepath.Join(e.dataDir, file.Name())
		entry, err := e.loadFromDiskFile(filePath)
		if err != nil {
			e.logger.Errorf("Failed to load file %s: %v", filePath, err)
			continue
		}

		e.diskIndex[entry.Key] = filePath
		e.logger.Debugf("Loaded key %s from disk", entry.Key)
	}

	return nil
}

// Close properly shuts down the storage engine
func (e *StorageEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Save all memory cache entries to disk
	for _, entry := range e.memCache {
		if err := e.saveToDisk(entry); err != nil {
			e.logger.Errorf("Failed to save entry to disk during shutdown: %v", err)
		}
	}

	e.compressor.Close()
	e.decompressor.Close()
	return nil
}

// Stats returns storage statistics
func (e *StorageEngine) Stats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"memory_entries": len(e.memCache),
		"disk_entries":   len(e.diskIndex),
		"total_entries":  len(e.memCache) + len(e.diskIndex),
	}
}
