package failure

import (
	"context"
	"sync"
	"time"

	"github.com/alexnthnz/distri-kv/internal/partition"
	"github.com/sirupsen/logrus"
)

// NodeStatus represents the status of a node
type NodeStatus int

const (
	StatusUnknown NodeStatus = iota
	StatusHealthy
	StatusSuspected
	StatusFailed
	StatusRecovered
)

func (s NodeStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusSuspected:
		return "suspected"
	case StatusFailed:
		return "failed"
	case StatusRecovered:
		return "recovered"
	default:
		return "unknown"
	}
}

// NodeHealth represents the health information of a node
type NodeHealth struct {
	NodeID           string        `json:"node_id"`
	Status           NodeStatus    `json:"status"`
	LastHeartbeat    time.Time     `json:"last_heartbeat"`
	LastStatusChange time.Time     `json:"last_status_change"`
	ConsecutiveFails int           `json:"consecutive_fails"`
	RTT              time.Duration `json:"rtt"` // Round trip time
	Version          int64         `json:"version"`
}

// FailureDetector implements failure detection using heartbeat and gossip protocols
type FailureDetector struct {
	nodeID              string
	nodes               map[string]*NodeHealth
	heartbeatInterval   time.Duration
	timeoutThreshold    time.Duration
	maxConsecutiveFails int
	logger              *logrus.Logger
	mu                  sync.RWMutex
	stopChan            chan struct{}
	isRunning           bool
	callbacks           []FailureCallback
}

// FailureCallback is called when a node's status changes
type FailureCallback func(nodeID string, oldStatus, newStatus NodeStatus)

// NewFailureDetector creates a new failure detector
func NewFailureDetector(nodeID string, logger *logrus.Logger) *FailureDetector {
	fd := &FailureDetector{
		nodeID:              nodeID,
		nodes:               make(map[string]*NodeHealth),
		heartbeatInterval:   5 * time.Second,
		timeoutThreshold:    15 * time.Second,
		maxConsecutiveFails: 3,
		logger:              logger,
		stopChan:            make(chan struct{}),
		callbacks:           make([]FailureCallback, 0),
	}

	return fd
}

// AddNode adds a node to monitor
func (fd *FailureDetector) AddNode(node *partition.Node) {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	fd.nodes[node.ID] = &NodeHealth{
		NodeID:           node.ID,
		Status:           StatusUnknown,
		LastHeartbeat:    time.Now(),
		LastStatusChange: time.Now(),
		ConsecutiveFails: 0,
		Version:          1,
	}

	fd.logger.Debugf("Added node %s to failure detector", node.ID)
}

// RemoveNode removes a node from monitoring
func (fd *FailureDetector) RemoveNode(nodeID string) {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	delete(fd.nodes, nodeID)
	fd.logger.Debugf("Removed node %s from failure detector", nodeID)
}

// Start starts the failure detector
func (fd *FailureDetector) Start() {
	if fd.isRunning {
		return
	}

	fd.isRunning = true
	go fd.heartbeatWorker()
	go fd.monitoringWorker()

	fd.logger.Info("Started failure detector")
}

// Stop stops the failure detector
func (fd *FailureDetector) Stop() {
	if !fd.isRunning {
		return
	}

	close(fd.stopChan)
	fd.isRunning = false
	fd.logger.Info("Stopped failure detector")
}

// heartbeatWorker sends periodic heartbeats to all nodes
func (fd *FailureDetector) heartbeatWorker() {
	ticker := time.NewTicker(fd.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fd.sendHeartbeats()
		case <-fd.stopChan:
			return
		}
	}
}

// monitoringWorker monitors node health and detects failures
func (fd *FailureDetector) monitoringWorker() {
	ticker := time.NewTicker(fd.heartbeatInterval / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fd.checkNodeHealth()
		case <-fd.stopChan:
			return
		}
	}
}

// sendHeartbeats sends heartbeat requests to all monitored nodes
func (fd *FailureDetector) sendHeartbeats() {
	fd.mu.RLock()
	nodes := make(map[string]*NodeHealth)
	for id, health := range fd.nodes {
		nodes[id] = health
	}
	fd.mu.RUnlock()

	for nodeID := range nodes {
		if nodeID == fd.nodeID {
			continue // Don't send heartbeat to self
		}

		go fd.sendHeartbeatToNode(nodeID)
	}
}

// sendHeartbeatToNode sends a heartbeat to a specific node
func (fd *FailureDetector) sendHeartbeatToNode(nodeID string) {
	start := time.Now()

	// In a real implementation, this would send an actual network request
	// For now, we'll simulate the heartbeat with a simple timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Simulate network request
	success := fd.simulateHeartbeat(ctx, nodeID)
	rtt := time.Since(start)

	fd.processHeartbeatResponse(nodeID, success, rtt)
}

// simulateHeartbeat simulates a heartbeat request (in real implementation, this would be an actual network call)
func (fd *FailureDetector) simulateHeartbeat(ctx context.Context, nodeID string) bool {
	// Simulate some nodes being temporarily unavailable
	select {
	case <-ctx.Done():
		return false
	case <-time.After(100 * time.Millisecond):
		// Simulate 95% success rate
		return time.Now().UnixNano()%100 < 95
	}
}

// processHeartbeatResponse processes the response from a heartbeat
func (fd *FailureDetector) processHeartbeatResponse(nodeID string, success bool, rtt time.Duration) {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	health, exists := fd.nodes[nodeID]
	if !exists {
		return
	}

	oldStatus := health.Status
	now := time.Now()

	if success {
		health.LastHeartbeat = now
		health.RTT = rtt
		health.ConsecutiveFails = 0

		// Update status based on previous state
		switch health.Status {
		case StatusUnknown, StatusSuspected, StatusFailed:
			health.Status = StatusHealthy
			health.LastStatusChange = now
		case StatusHealthy:
			// Already healthy, no change
		}
	} else {
		health.ConsecutiveFails++

		// Update status based on consecutive failures
		if health.ConsecutiveFails >= fd.maxConsecutiveFails {
			if health.Status != StatusFailed {
				health.Status = StatusFailed
				health.LastStatusChange = now
			}
		} else if health.Status == StatusHealthy {
			health.Status = StatusSuspected
			health.LastStatusChange = now
		}
	}

	health.Version++

	// Trigger callbacks if status changed
	if oldStatus != health.Status {
		fd.logger.Infof("Node %s status changed from %s to %s",
			nodeID, oldStatus.String(), health.Status.String())

		for _, callback := range fd.callbacks {
			go callback(nodeID, oldStatus, health.Status)
		}
	}
}

// checkNodeHealth checks for nodes that haven't sent heartbeats recently
func (fd *FailureDetector) checkNodeHealth() {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	now := time.Now()

	for nodeID, health := range fd.nodes {
		if nodeID == fd.nodeID {
			continue // Skip self
		}

		timeSinceLastHeartbeat := now.Sub(health.LastHeartbeat)

		// Check if node has been silent for too long
		if timeSinceLastHeartbeat > fd.timeoutThreshold {
			oldStatus := health.Status

			if health.Status != StatusFailed {
				health.Status = StatusFailed
				health.LastStatusChange = now
				health.Version++

				fd.logger.Warnf("Node %s failed due to timeout (last heartbeat: %v ago)",
					nodeID, timeSinceLastHeartbeat)

				// Trigger callbacks
				for _, callback := range fd.callbacks {
					go callback(nodeID, oldStatus, health.Status)
				}
			}
		}
	}
}

// GetNodeStatus returns the status of a specific node
func (fd *FailureDetector) GetNodeStatus(nodeID string) (NodeStatus, bool) {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	health, exists := fd.nodes[nodeID]
	if !exists {
		return StatusUnknown, false
	}

	return health.Status, true
}

// GetNodeHealth returns the health information of a specific node
func (fd *FailureDetector) GetNodeHealth(nodeID string) (*NodeHealth, bool) {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	health, exists := fd.nodes[nodeID]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	return &NodeHealth{
		NodeID:           health.NodeID,
		Status:           health.Status,
		LastHeartbeat:    health.LastHeartbeat,
		LastStatusChange: health.LastStatusChange,
		ConsecutiveFails: health.ConsecutiveFails,
		RTT:              health.RTT,
		Version:          health.Version,
	}, true
}

// GetAllNodeHealth returns health information for all nodes
func (fd *FailureDetector) GetAllNodeHealth() map[string]*NodeHealth {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	result := make(map[string]*NodeHealth)
	for nodeID, health := range fd.nodes {
		result[nodeID] = &NodeHealth{
			NodeID:           health.NodeID,
			Status:           health.Status,
			LastHeartbeat:    health.LastHeartbeat,
			LastStatusChange: health.LastStatusChange,
			ConsecutiveFails: health.ConsecutiveFails,
			RTT:              health.RTT,
			Version:          health.Version,
		}
	}

	return result
}

// GetHealthyNodes returns a list of healthy node IDs
func (fd *FailureDetector) GetHealthyNodes() []string {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	var healthyNodes []string
	for nodeID, health := range fd.nodes {
		if health.Status == StatusHealthy {
			healthyNodes = append(healthyNodes, nodeID)
		}
	}

	return healthyNodes
}

// GetFailedNodes returns a list of failed node IDs
func (fd *FailureDetector) GetFailedNodes() []string {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	var failedNodes []string
	for nodeID, health := range fd.nodes {
		if health.Status == StatusFailed {
			failedNodes = append(failedNodes, nodeID)
		}
	}

	return failedNodes
}

// IsNodeHealthy returns true if the node is healthy
func (fd *FailureDetector) IsNodeHealthy(nodeID string) bool {
	status, exists := fd.GetNodeStatus(nodeID)
	return exists && status == StatusHealthy
}

// AddCallback adds a callback function to be called when node status changes
func (fd *FailureDetector) AddCallback(callback FailureCallback) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.callbacks = append(fd.callbacks, callback)
}

// UpdateHeartbeat manually updates the heartbeat for a node (used when receiving heartbeats)
func (fd *FailureDetector) UpdateHeartbeat(nodeID string) {
	fd.mu.Lock()
	defer fd.mu.Unlock()

	health, exists := fd.nodes[nodeID]
	if !exists {
		// Auto-add unknown nodes
		health = &NodeHealth{
			NodeID:           nodeID,
			Status:           StatusHealthy,
			LastHeartbeat:    time.Now(),
			LastStatusChange: time.Now(),
			ConsecutiveFails: 0,
			Version:          1,
		}
		fd.nodes[nodeID] = health
		fd.logger.Debugf("Auto-added node %s to failure detector", nodeID)
		return
	}

	oldStatus := health.Status
	now := time.Now()

	health.LastHeartbeat = now
	health.ConsecutiveFails = 0

	// Update status if node was previously failed or suspected
	if health.Status != StatusHealthy {
		health.Status = StatusHealthy
		health.LastStatusChange = now
		health.Version++

		fd.logger.Infof("Node %s recovered (status changed from %s to %s)",
			nodeID, oldStatus.String(), health.Status.String())

		// Trigger callbacks
		for _, callback := range fd.callbacks {
			go callback(nodeID, oldStatus, health.Status)
		}
	}
}

// GetStats returns failure detector statistics
func (fd *FailureDetector) GetStats() map[string]interface{} {
	fd.mu.RLock()
	defer fd.mu.RUnlock()

	statusCounts := make(map[string]int)
	var totalRTT time.Duration
	healthyNodes := 0

	for _, health := range fd.nodes {
		statusCounts[health.Status.String()]++
		if health.Status == StatusHealthy {
			totalRTT += health.RTT
			healthyNodes++
		}
	}

	var avgRTT time.Duration
	if healthyNodes > 0 {
		avgRTT = totalRTT / time.Duration(healthyNodes)
	}

	return map[string]interface{}{
		"total_nodes":           len(fd.nodes),
		"healthy_nodes":         statusCounts["healthy"],
		"suspected_nodes":       statusCounts["suspected"],
		"failed_nodes":          statusCounts["failed"],
		"unknown_nodes":         statusCounts["unknown"],
		"heartbeat_interval":    fd.heartbeatInterval.String(),
		"timeout_threshold":     fd.timeoutThreshold.String(),
		"max_consecutive_fails": fd.maxConsecutiveFails,
		"average_rtt":           avgRTT.String(),
		"is_running":            fd.isRunning,
		"callbacks_registered":  len(fd.callbacks),
	}
}

// SetHeartbeatInterval sets the heartbeat interval
func (fd *FailureDetector) SetHeartbeatInterval(interval time.Duration) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.heartbeatInterval = interval
}

// SetTimeoutThreshold sets the timeout threshold for failure detection
func (fd *FailureDetector) SetTimeoutThreshold(threshold time.Duration) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.timeoutThreshold = threshold
}

// SetMaxConsecutiveFails sets the maximum consecutive failures before marking a node as failed
func (fd *FailureDetector) SetMaxConsecutiveFails(maxFails int) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.maxConsecutiveFails = maxFails
}
