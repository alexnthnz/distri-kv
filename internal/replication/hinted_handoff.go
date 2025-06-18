package replication

import (
	"context"
	"sync"
	"time"

	"github.com/alexnthnz/distri-kv/internal/versioning"
	"github.com/sirupsen/logrus"
)

// Hint represents a hinted write for a temporarily unavailable node
type Hint struct {
	Key          string                  `json:"key"`
	Value        []byte                  `json:"value"`
	TargetNodeID string                  `json:"target_node_id"`
	VectorClock  *versioning.VectorClock `json:"vector_clock"`
	Timestamp    time.Time               `json:"timestamp"`
	RetryCount   int                     `json:"retry_count"`
	MaxRetries   int                     `json:"max_retries"`
}

// HintedHandoff manages hinted handoff for temporary failures
type HintedHandoff struct {
	hints         map[string][]*Hint // targetNodeID -> []Hint
	maxHints      int
	maxRetries    int
	retryInterval time.Duration
	hintTTL       time.Duration
	logger        *logrus.Logger
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     bool
}

// NewHintedHandoff creates a new hinted handoff manager
func NewHintedHandoff(logger *logrus.Logger) *HintedHandoff {
	hh := &HintedHandoff{
		hints:         make(map[string][]*Hint),
		maxHints:      10000, // Maximum hints to store
		maxRetries:    3,     // Maximum retry attempts
		retryInterval: 30 * time.Second,
		hintTTL:       24 * time.Hour, // Hints expire after 24 hours
		logger:        logger,
		stopChan:      make(chan struct{}),
	}

	// Start background goroutine for hint delivery
	go hh.deliveryWorker()
	hh.isRunning = true

	return hh
}

// StoreHint stores a hint for later delivery
func (hh *HintedHandoff) StoreHint(key string, value []byte, targetNodeIDs []string, vclock *versioning.VectorClock) {
	hh.mu.Lock()
	defer hh.mu.Unlock()

	for _, nodeID := range targetNodeIDs {
		// Check if we've reached the maximum number of hints
		if hh.getTotalHintCount() >= hh.maxHints {
			hh.logger.Warnf("Maximum hint count reached, dropping hint for node %s", nodeID)
			continue
		}

		hint := &Hint{
			Key:          key,
			Value:        value,
			TargetNodeID: nodeID,
			VectorClock:  vclock.Clone(),
			Timestamp:    time.Now(),
			RetryCount:   0,
			MaxRetries:   hh.maxRetries,
		}

		hh.hints[nodeID] = append(hh.hints[nodeID], hint)
		hh.logger.Debugf("Stored hint for key %s to node %s", key, nodeID)
	}
}

// getTotalHintCount returns the total number of hints (must be called with lock held)
func (hh *HintedHandoff) getTotalHintCount() int {
	count := 0
	for _, hints := range hh.hints {
		count += len(hints)
	}
	return count
}

// deliveryWorker runs in the background to deliver hints
func (hh *HintedHandoff) deliveryWorker() {
	ticker := time.NewTicker(hh.retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hh.attemptHintDelivery()
		case <-hh.stopChan:
			hh.logger.Info("Stopping hinted handoff delivery worker")
			return
		}
	}
}

// attemptHintDelivery attempts to deliver all stored hints
func (hh *HintedHandoff) attemptHintDelivery() {
	hh.mu.Lock()
	defer hh.mu.Unlock()

	now := time.Now()

	for nodeID, hints := range hh.hints {
		if len(hints) == 0 {
			continue
		}

		// Filter out expired hints
		var validHints []*Hint
		for _, hint := range hints {
			if now.Sub(hint.Timestamp) <= hh.hintTTL {
				validHints = append(validHints, hint)
			} else {
				hh.logger.Debugf("Hint expired for key %s to node %s", hint.Key, nodeID)
			}
		}

		if len(validHints) == 0 {
			delete(hh.hints, nodeID)
			continue
		}

		hh.hints[nodeID] = validHints

		// Try to deliver hints (this would typically involve checking if the node is back online)
		// For now, we'll simulate hint delivery by logging
		hh.logger.Debugf("Attempting to deliver %d hints to node %s", len(validHints), nodeID)

		// In a real implementation, you would:
		// 1. Check if the target node is back online
		// 2. Attempt to deliver each hint
		// 3. Remove successfully delivered hints
		// 4. Increment retry count for failed deliveries
		// 5. Remove hints that exceed max retries
	}
}

// DeliverHintsToNode delivers all hints for a specific node (called when node comes back online)
func (hh *HintedHandoff) DeliverHintsToNode(nodeID string, nodeClient NodeClient) error {
	hh.mu.Lock()
	hints, exists := hh.hints[nodeID]
	if !exists || len(hints) == 0 {
		hh.mu.Unlock()
		return nil
	}

	// Create a copy of hints to avoid holding the lock too long
	hintsCopy := make([]*Hint, len(hints))
	copy(hintsCopy, hints)
	hh.mu.Unlock()

	successfullyDelivered := make(map[int]bool)

	for i, hint := range hintsCopy {
		// Create replication request
		req := &ReplicationRequest{
			Key:    hint.Key,
			Value:  hint.Value,
			NodeID: "hint-delivery", // Special node ID for hint delivery
			VClock: hint.VectorClock,
		}

		// Attempt delivery with timeout
		ctx, cancel := createTimeoutContext(5 * time.Second)
		resp, err := nodeClient.Put(ctx, req)
		cancel()

		if err != nil || !resp.Success {
			hint.RetryCount++
			hh.logger.Errorf("Failed to deliver hint to node %s (attempt %d): %v",
				nodeID, hint.RetryCount, err)
		} else {
			successfullyDelivered[i] = true
			hh.logger.Debugf("Successfully delivered hint for key %s to node %s",
				hint.Key, nodeID)
		}
	}

	// Remove successfully delivered hints and update retry counts
	hh.mu.Lock()
	defer hh.mu.Unlock()

	var remainingHints []*Hint
	for i, hint := range hh.hints[nodeID] {
		if successfullyDelivered[i] {
			continue // Skip successfully delivered hints
		}

		if hint.RetryCount >= hint.MaxRetries {
			hh.logger.Warnf("Dropping hint for key %s to node %s after %d retries",
				hint.Key, nodeID, hint.RetryCount)
			continue // Skip hints that have exceeded max retries
		}

		remainingHints = append(remainingHints, hint)
	}

	if len(remainingHints) == 0 {
		delete(hh.hints, nodeID)
	} else {
		hh.hints[nodeID] = remainingHints
	}

	return nil
}

// GetHintsForNode returns all hints for a specific node
func (hh *HintedHandoff) GetHintsForNode(nodeID string) []*Hint {
	hh.mu.RLock()
	defer hh.mu.RUnlock()

	hints, exists := hh.hints[nodeID]
	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	result := make([]*Hint, len(hints))
	copy(result, hints)
	return result
}

// GetStats returns hinted handoff statistics
func (hh *HintedHandoff) GetStats() map[string]interface{} {
	hh.mu.RLock()
	defer hh.mu.RUnlock()

	nodeStats := make(map[string]int)
	totalHints := 0

	for nodeID, hints := range hh.hints {
		nodeStats[nodeID] = len(hints)
		totalHints += len(hints)
	}

	return map[string]interface{}{
		"total_hints":      totalHints,
		"nodes_with_hints": len(nodeStats),
		"hints_per_node":   nodeStats,
		"max_hints":        hh.maxHints,
		"max_retries":      hh.maxRetries,
		"retry_interval":   hh.retryInterval.String(),
		"hint_ttl":         hh.hintTTL.String(),
		"is_running":       hh.isRunning,
	}
}

// ClearHints clears all hints for a specific node
func (hh *HintedHandoff) ClearHints(nodeID string) {
	hh.mu.Lock()
	defer hh.mu.Unlock()

	count := len(hh.hints[nodeID])
	delete(hh.hints, nodeID)

	hh.logger.Infof("Cleared %d hints for node %s", count, nodeID)
}

// ClearAllHints clears all stored hints
func (hh *HintedHandoff) ClearAllHints() {
	hh.mu.Lock()
	defer hh.mu.Unlock()

	totalHints := hh.getTotalHintCount()
	hh.hints = make(map[string][]*Hint)

	hh.logger.Infof("Cleared all %d hints", totalHints)
}

// Stop stops the hinted handoff delivery worker
func (hh *HintedHandoff) Stop() {
	if hh.isRunning {
		close(hh.stopChan)
		hh.isRunning = false
		hh.logger.Info("Stopped hinted handoff manager")
	}
}

// SetMaxHints sets the maximum number of hints to store
func (hh *HintedHandoff) SetMaxHints(maxHints int) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	hh.maxHints = maxHints
}

// SetRetryInterval sets the retry interval for hint delivery
func (hh *HintedHandoff) SetRetryInterval(interval time.Duration) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	hh.retryInterval = interval
}

// SetHintTTL sets the time-to-live for hints
func (hh *HintedHandoff) SetHintTTL(ttl time.Duration) {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	hh.hintTTL = ttl
}

// Helper function to create timeout context (would be imported from context package in real implementation)
func createTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	// In a real implementation, this would be:
	// return context.WithTimeout(context.Background(), timeout)
	// For now, we'll use a simple channel-based approach
	ctx := &timeoutContext{
		done: make(chan struct{}),
	}

	cancel := func() {
		select {
		case <-ctx.done:
		default:
			close(ctx.done)
		}
	}

	go func() {
		time.Sleep(timeout)
		cancel()
	}()

	return ctx, cancel
}

// Simple timeout context implementation for demonstration
type timeoutContext struct {
	done chan struct{}
}

func (ctx *timeoutContext) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

func (ctx *timeoutContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *timeoutContext) Err() error {
	select {
	case <-ctx.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func (ctx *timeoutContext) Value(key interface{}) interface{} {
	return nil
}
