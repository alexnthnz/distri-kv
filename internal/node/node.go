package node

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alexnthnz/distri-kv/internal/failure"
	"github.com/alexnthnz/distri-kv/internal/partition"
	"github.com/alexnthnz/distri-kv/internal/replication"
	"github.com/alexnthnz/distri-kv/internal/storage"
	"github.com/alexnthnz/distri-kv/internal/versioning"
	"github.com/sirupsen/logrus"
)

// Config represents the configuration for a node
type Config struct {
	NodeID        string                    `yaml:"node_id"`
	Address       string                    `yaml:"address"`
	DataDir       string                    `yaml:"data_dir"`
	MaxMemItems   int                       `yaml:"max_mem_items"`
	QuorumConfig  *replication.QuorumConfig `yaml:"quorum"`
	ClusterNodes  []string                  `yaml:"cluster_nodes"`
	BootstrapNode string                    `yaml:"bootstrap_node"`
}

// Node represents a distributed key-value store node
type Node struct {
	config           *Config
	id               string
	address          string
	storage          *storage.StorageEngine
	partitionManager *partition.PartitionManager
	quorumManager    *replication.QuorumManager
	failureDetector  *failure.FailureDetector
	nodeClients      map[string]replication.NodeClient
	logger           *logrus.Logger
	mu               sync.RWMutex
	isRunning        bool
	stopChan         chan struct{}
}

// NewNode creates a new distributed key-value store node
func NewNode(config *Config, logger *logrus.Logger) (*Node, error) {
	// Validate configuration
	if config.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}
	if config.Address == "" {
		return nil, fmt.Errorf("node address is required")
	}
	if config.DataDir == "" {
		config.DataDir = fmt.Sprintf("./data/%s", config.NodeID)
	}
	if config.MaxMemItems <= 0 {
		config.MaxMemItems = 1000
	}

	// Create storage engine
	storageEngine, err := storage.NewStorageEngine(config.DataDir, config.MaxMemItems, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage engine: %w", err)
	}

	// Create partition manager
	partitionManager := partition.NewPartitionManager(config.QuorumConfig.N)

	// Create quorum manager
	quorumManager, err := replication.NewQuorumManager(config.QuorumConfig, partitionManager, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create quorum manager: %w", err)
	}

	// Create failure detector
	failureDetector := failure.NewFailureDetector(config.NodeID, logger)

	node := &Node{
		config:           config,
		id:               config.NodeID,
		address:          config.Address,
		storage:          storageEngine,
		partitionManager: partitionManager,
		quorumManager:    quorumManager,
		failureDetector:  failureDetector,
		nodeClients:      make(map[string]replication.NodeClient),
		logger:           logger,
		stopChan:         make(chan struct{}),
	}

	// Setup failure detector callbacks
	failureDetector.AddCallback(node.onNodeStatusChange)

	return node, nil
}

// Start starts the node
func (n *Node) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.isRunning {
		return fmt.Errorf("node is already running")
	}

	n.logger.Infof("Starting node %s at %s", n.id, n.address)

	// Start failure detector
	n.failureDetector.Start()

	// Join the cluster
	if err := n.joinCluster(); err != nil {
		return fmt.Errorf("failed to join cluster: %w", err)
	}

	n.isRunning = true
	n.logger.Infof("Node %s started successfully", n.id)

	return nil
}

// Stop stops the node
func (n *Node) Stop() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.isRunning {
		return nil
	}

	n.logger.Infof("Stopping node %s", n.id)

	// Stop failure detector
	n.failureDetector.Stop()

	// Close storage engine
	if err := n.storage.Close(); err != nil {
		n.logger.Errorf("Error closing storage engine: %v", err)
	}

	// Signal stop
	close(n.stopChan)
	n.isRunning = false

	n.logger.Infof("Node %s stopped", n.id)
	return nil
}

// joinCluster joins the node to the cluster
func (n *Node) joinCluster() error {
	// Add self to partition manager
	selfNode := &partition.Node{
		ID:      n.id,
		Address: n.address,
		Weight:  1,
	}
	n.partitionManager.AddNode(selfNode)

	// Register local node with quorum manager for direct local operations
	n.quorumManager.SetLocalNode(n.id, n)

	// Add other cluster nodes
	for _, nodeAddr := range n.config.ClusterNodes {
		if nodeAddr != n.address {
			// In a real implementation, you would discover node IDs
			// For now, we'll use the address as the ID
			nodeID := nodeAddr
			clusterNode := &partition.Node{
				ID:      nodeID,
				Address: nodeAddr,
				Weight:  1,
			}

			n.partitionManager.AddNode(clusterNode)
			n.failureDetector.AddNode(clusterNode)

			// Create node client (in a real implementation, this would be a gRPC client)
			nodeClient := &MockNodeClient{nodeID: nodeID, logger: n.logger}
			n.nodeClients[nodeID] = nodeClient
			n.quorumManager.RegisterNodeClient(nodeID, nodeClient)
		}
	}

	return nil
}

// onNodeStatusChange handles node status changes
func (n *Node) onNodeStatusChange(nodeID string, oldStatus, newStatus failure.NodeStatus) {
	n.logger.Infof("Node %s status changed: %s -> %s", nodeID, oldStatus.String(), newStatus.String())

	switch newStatus {
	case failure.StatusHealthy:
		// Node recovered - deliver any pending hints
		if nodeClient, exists := n.nodeClients[nodeID]; exists {
			go func() {
				err := n.quorumManager.GetHintedHandoff().DeliverHintsToNode(nodeID, nodeClient)
				if err != nil {
					n.logger.Errorf("Failed to deliver hints to recovered node %s: %v", nodeID, err)
				}
			}()
		}
	case failure.StatusFailed:
		// Node failed - may need to trigger rebalancing or other recovery mechanisms
		n.logger.Warnf("Node %s has failed, cluster may need rebalancing", nodeID)
	}
}

// Put stores a key-value pair
func (n *Node) Put(ctx context.Context, key string, value []byte) error {
	if !n.isRunning {
		return fmt.Errorf("node is not running")
	}

	n.logger.Debugf("PUT request for key: %s", key)

	// Store locally first
	if err := n.storage.Put(key, value); err != nil {
		return fmt.Errorf("failed to store locally: %w", err)
	}

	// Replicate using quorum consensus
	if err := n.quorumManager.Put(ctx, key, value, n.id); err != nil {
		// If replication fails, we should still keep the local copy
		n.logger.Errorf("Replication failed for key %s: %v", key, err)
		return fmt.Errorf("replication failed: %w", err)
	}

	return nil
}

// Get retrieves a value by key
func (n *Node) Get(ctx context.Context, key string) ([]byte, error) {
	if !n.isRunning {
		return nil, fmt.Errorf("node is not running")
	}

	n.logger.Debugf("GET request for key: %s", key)

	// Try local storage first for performance
	if value, exists, err := n.storage.Get(key); err != nil {
		n.logger.Errorf("Error reading from local storage: %v", err)
	} else if exists {
		n.logger.Debugf("Found key %s in local storage", key)
		return value, nil
	}

	// Use quorum read for consistency
	value, _, err := n.quorumManager.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("quorum read failed: %w", err)
	}

	// Cache the result locally
	if value != nil {
		if err := n.storage.Put(key, value); err != nil {
			n.logger.Errorf("Failed to cache value locally: %v", err)
		}
	}

	return value, nil
}

// Delete removes a key-value pair
func (n *Node) Delete(ctx context.Context, key string) error {
	if !n.isRunning {
		return fmt.Errorf("node is not running")
	}

	n.logger.Debugf("DELETE request for key: %s", key)

	// Delete locally
	if err := n.storage.Delete(key); err != nil {
		n.logger.Errorf("Failed to delete locally: %v", err)
	}

	// Delete using quorum consensus
	if err := n.quorumManager.Delete(ctx, key); err != nil {
		return fmt.Errorf("quorum delete failed: %w", err)
	}

	return nil
}

// LocalPut stores a key-value pair locally (used by replication)
func (n *Node) LocalPut(ctx context.Context, req *replication.ReplicationRequest) (*replication.ReplicationResponse, error) {
	if !n.isRunning {
		return &replication.ReplicationResponse{
			Success: false,
			NodeID:  n.id,
			Error:   fmt.Errorf("node is not running"),
		}, nil
	}

	// Store the value locally
	err := n.storage.Put(req.Key, req.Value)
	if err != nil {
		return &replication.ReplicationResponse{
			Success: false,
			NodeID:  n.id,
			Error:   fmt.Errorf("failed to store locally: %w", err),
		}, nil
	}

	// Update vector clock
	vclock := req.VClock.Clone()
	vclock.Increment(n.id)

	return &replication.ReplicationResponse{
		Success:   true,
		NodeID:    n.id,
		VClock:    vclock,
		Timestamp: time.Now(),
	}, nil
}

// LocalGet retrieves a value locally (used by replication)
func (n *Node) LocalGet(ctx context.Context, req *replication.ReadRequest) (*replication.ReadResponse, error) {
	if !n.isRunning {
		return &replication.ReadResponse{
			Success: false,
			NodeID:  n.id,
			Error:   fmt.Errorf("node is not running"),
		}, nil
	}

	value, exists, err := n.storage.Get(req.Key)
	if err != nil {
		return &replication.ReadResponse{
			Success: false,
			NodeID:  n.id,
			Error:   fmt.Errorf("failed to read locally: %w", err),
		}, nil
	}

	if !exists {
		return &replication.ReadResponse{
			Success: false,
			NodeID:  n.id,
			Error:   fmt.Errorf("key not found"),
		}, nil
	}

	// Create a vector clock for this read
	vclock := versioning.NewVectorClock()
	vclock.Increment(n.id)

	return &replication.ReadResponse{
		Success: true,
		NodeID:  n.id,
		Value:   value,
		VClock:  vclock,
	}, nil
}

// LocalDelete deletes a key locally (used by replication)
func (n *Node) LocalDelete(ctx context.Context, key string) error {
	if !n.isRunning {
		return fmt.Errorf("node is not running")
	}

	return n.storage.Delete(key)
}

// IsHealthy returns true if the node is healthy
func (n *Node) IsHealthy(ctx context.Context) bool {
	return n.isRunning
}

// GetStats returns node statistics
func (n *Node) GetStats() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	stats := map[string]interface{}{
		"node_id":         n.id,
		"address":         n.address,
		"is_running":      n.isRunning,
		"storage_stats":   n.storage.Stats(),
		"partition_stats": n.partitionManager.GetStats(),
		"quorum_stats":    n.quorumManager.GetStats(),
		"failure_stats":   n.failureDetector.GetStats(),
	}

	return stats
}

// GetNodeID returns the node ID
func (n *Node) GetNodeID() string {
	return n.id
}

// GetAddress returns the node address
func (n *Node) GetAddress() string {
	return n.address
}

// MockNodeClient is a mock implementation of NodeClient for testing
type MockNodeClient struct {
	nodeID  string
	logger  *logrus.Logger
	storage map[string][]byte // Simple in-memory storage for testing
	mu      sync.RWMutex
}

func (c *MockNodeClient) Put(ctx context.Context, req *replication.ReplicationRequest) (*replication.ReplicationResponse, error) {
	// Simulate network delay
	time.Sleep(10 * time.Millisecond)

	// Simulate 95% success rate
	if time.Now().UnixNano()%100 < 95 {
		c.mu.Lock()
		if c.storage == nil {
			c.storage = make(map[string][]byte)
		}
		c.storage[req.Key] = req.Value
		c.mu.Unlock()

		vclock := req.VClock.Clone()
		vclock.Increment(c.nodeID)

		return &replication.ReplicationResponse{
			Success:   true,
			NodeID:    c.nodeID,
			VClock:    vclock,
			Timestamp: time.Now(),
		}, nil
	}

	return &replication.ReplicationResponse{
		Success: false,
		NodeID:  c.nodeID,
		Error:   fmt.Errorf("simulated failure"),
	}, nil
}

func (c *MockNodeClient) Get(ctx context.Context, req *replication.ReadRequest) (*replication.ReadResponse, error) {
	// Simulate network delay
	time.Sleep(10 * time.Millisecond)

	// Simulate 95% success rate
	if time.Now().UnixNano()%100 < 95 {
		c.mu.RLock()
		value, exists := c.storage[req.Key]
		c.mu.RUnlock()

		if !exists {
			return &replication.ReadResponse{
				Success: false,
				NodeID:  c.nodeID,
				Error:   fmt.Errorf("key not found"),
			}, nil
		}

		vclock := versioning.NewVectorClock()
		vclock.Increment(c.nodeID)

		return &replication.ReadResponse{
			Success: true,
			NodeID:  c.nodeID,
			Value:   value,
			VClock:  vclock,
		}, nil
	}

	return &replication.ReadResponse{
		Success: false,
		NodeID:  c.nodeID,
		Error:   fmt.Errorf("simulated failure"),
	}, nil
}

func (c *MockNodeClient) Delete(ctx context.Context, key string) error {
	// Simulate network delay
	time.Sleep(10 * time.Millisecond)

	// Simulate 95% success rate
	if time.Now().UnixNano()%100 < 95 {
		c.mu.Lock()
		if c.storage != nil {
			delete(c.storage, key)
		}
		c.mu.Unlock()
		return nil
	}

	return fmt.Errorf("simulated failure")
}

func (c *MockNodeClient) IsHealthy(ctx context.Context) bool {
	// Simulate 95% health rate
	return time.Now().UnixNano()%100 < 95
}
