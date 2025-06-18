package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alexnthnz/distri-kv/internal/partition"
	"github.com/alexnthnz/distri-kv/internal/versioning"
	"github.com/sirupsen/logrus"
)

// QuorumConfig defines the quorum configuration
type QuorumConfig struct {
	N int // Replication factor (total replicas)
	R int // Read quorum (minimum nodes for read)
	W int // Write quorum (minimum nodes for write)
}

// ValidateQuorum validates the quorum configuration
func (qc *QuorumConfig) ValidateQuorum() error {
	if qc.N <= 0 {
		return fmt.Errorf("replication factor N must be positive")
	}
	if qc.R <= 0 || qc.R > qc.N {
		return fmt.Errorf("read quorum R must be between 1 and N")
	}
	if qc.W <= 0 || qc.W > qc.N {
		return fmt.Errorf("write quorum W must be between 1 and N")
	}
	return nil
}

// IsStronglyConsistent returns true if the configuration guarantees strong consistency
func (qc *QuorumConfig) IsStronglyConsistent() bool {
	return qc.R+qc.W > qc.N
}

// ReplicationRequest represents a replication request
type ReplicationRequest struct {
	Key    string
	Value  []byte
	NodeID string
	VClock *versioning.VectorClock
}

// ReplicationResponse represents a replication response
type ReplicationResponse struct {
	Success   bool
	NodeID    string
	VClock    *versioning.VectorClock
	Error     error
	Timestamp time.Time
}

// ReadRequest represents a read request
type ReadRequest struct {
	Key string
}

// ReadResponse represents a read response
type ReadResponse struct {
	Success bool
	NodeID  string
	Value   []byte
	VClock  *versioning.VectorClock
	Error   error
}

// NodeClient represents a client for communicating with a node
type NodeClient interface {
	Put(ctx context.Context, req *ReplicationRequest) (*ReplicationResponse, error)
	Get(ctx context.Context, req *ReadRequest) (*ReadResponse, error)
	Delete(ctx context.Context, key string) error
	IsHealthy(ctx context.Context) bool
}

// QuorumManager manages quorum operations
type QuorumManager struct {
	config        *QuorumConfig
	nodeClients   map[string]NodeClient
	partitioner   *partition.PartitionManager
	resolver      versioning.ConflictResolver
	hintedHandoff *HintedHandoff
	logger        *logrus.Logger
	localNodeID   string
	localNode     LocalNodeInterface
	mu            sync.RWMutex
}

// LocalNodeInterface defines the interface for local node operations
type LocalNodeInterface interface {
	LocalPut(ctx context.Context, req *ReplicationRequest) (*ReplicationResponse, error)
	LocalGet(ctx context.Context, req *ReadRequest) (*ReadResponse, error)
	LocalDelete(ctx context.Context, key string) error
}

// NewQuorumManager creates a new quorum manager
func NewQuorumManager(config *QuorumConfig, partitioner *partition.PartitionManager, logger *logrus.Logger) (*QuorumManager, error) {
	if err := config.ValidateQuorum(); err != nil {
		return nil, fmt.Errorf("invalid quorum config: %w", err)
	}

	return &QuorumManager{
		config:        config,
		nodeClients:   make(map[string]NodeClient),
		partitioner:   partitioner,
		resolver:      &versioning.LastWriteWinsResolver{},
		hintedHandoff: NewHintedHandoff(logger),
		logger:        logger,
	}, nil
}

// RegisterNodeClient registers a client for a node
func (qm *QuorumManager) RegisterNodeClient(nodeID string, client NodeClient) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.nodeClients[nodeID] = client
}

// SetLocalNode sets the local node reference for direct local operations
func (qm *QuorumManager) SetLocalNode(nodeID string, localNode LocalNodeInterface) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.localNodeID = nodeID
	qm.localNode = localNode
}

// Put performs a quorum write operation
func (qm *QuorumManager) Put(ctx context.Context, key string, value []byte, nodeID string) error {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	// Get nodes responsible for this key
	partition, err := qm.partitioner.GetPartitionForKey(key)
	if err != nil {
		return fmt.Errorf("failed to get partition: %w", err)
	}

	// Create versioned value
	versionedValue := versioning.NewVersionedValue(key, value, nodeID)

	// Prepare replication request
	req := &ReplicationRequest{
		Key:    key,
		Value:  value,
		NodeID: nodeID,
		VClock: versionedValue.VectorClock,
	}

	// Send requests to all replica nodes
	responses := make(chan *ReplicationResponse, len(partition.Nodes))
	successCount := 0

	for _, targetNodeID := range partition.Nodes {
		go func(targetNodeID string) {
			// Handle local node operations directly
			if targetNodeID == qm.localNodeID && qm.localNode != nil {
				resp, err := qm.localNode.LocalPut(ctx, req)
				if err != nil {
					resp = &ReplicationResponse{
						Success: false,
						NodeID:  targetNodeID,
						Error:   err,
					}
				}
				responses <- resp
				return
			}

			// Handle remote node operations via client
			client, exists := qm.nodeClients[targetNodeID]
			if !exists {
				responses <- &ReplicationResponse{
					Success: false,
					NodeID:  targetNodeID,
					Error:   fmt.Errorf("no client for node %s", targetNodeID),
				}
				return
			}

			resp, err := client.Put(ctx, req)
			if err != nil {
				resp = &ReplicationResponse{
					Success: false,
					NodeID:  targetNodeID,
					Error:   err,
				}
			}
			responses <- resp
		}(targetNodeID)
	}

	// Collect responses
	var failedNodes []string
	var successfulResponses []*ReplicationResponse

	for i := 0; i < len(partition.Nodes); i++ {
		select {
		case resp := <-responses:
			if resp.Success {
				successCount++
				successfulResponses = append(successfulResponses, resp)
			} else {
				failedNodes = append(failedNodes, resp.NodeID)
				qm.logger.Errorf("Replication failed for node %s: %v", resp.NodeID, resp.Error)
			}
		case <-ctx.Done():
			return fmt.Errorf("write operation timed out")
		}
	}

	// Check if we achieved write quorum
	if successCount >= qm.config.W {
		qm.logger.Debugf("Write quorum achieved for key %s (%d/%d nodes)", key, successCount, qm.config.W)

		// Handle failed nodes with hinted handoff
		if len(failedNodes) > 0 {
			qm.hintedHandoff.StoreHint(key, value, failedNodes, versionedValue.VectorClock)
		}

		return nil
	}

	return fmt.Errorf("write quorum not achieved: %d/%d nodes responded successfully", successCount, qm.config.W)
}

// Get performs a quorum read operation
func (qm *QuorumManager) Get(ctx context.Context, key string) ([]byte, *versioning.VectorClock, error) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	// Get nodes responsible for this key
	partition, err := qm.partitioner.GetPartitionForKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get partition: %w", err)
	}

	// Send read requests to all replica nodes
	responses := make(chan *ReadResponse, len(partition.Nodes))

	for _, nodeID := range partition.Nodes {
		go func(nodeID string) {
			// Handle local node operations directly
			if nodeID == qm.localNodeID && qm.localNode != nil {
				req := &ReadRequest{Key: key}
				resp, err := qm.localNode.LocalGet(ctx, req)
				if err != nil {
					resp = &ReadResponse{
						Success: false,
						NodeID:  nodeID,
						Error:   err,
					}
				}
				responses <- resp
				return
			}

			// Handle remote node operations via client
			client, exists := qm.nodeClients[nodeID]
			if !exists {
				responses <- &ReadResponse{
					Success: false,
					NodeID:  nodeID,
					Error:   fmt.Errorf("no client for node %s", nodeID),
				}
				return
			}

			req := &ReadRequest{Key: key}
			resp, err := client.Get(ctx, req)
			if err != nil {
				resp = &ReadResponse{
					Success: false,
					NodeID:  nodeID,
					Error:   err,
				}
			}
			responses <- resp
		}(nodeID)
	}

	// Collect responses
	var validResponses []*ReadResponse
	successCount := 0

	for i := 0; i < len(partition.Nodes); i++ {
		select {
		case resp := <-responses:
			if resp.Success && resp.Value != nil {
				validResponses = append(validResponses, resp)
				successCount++
			} else if resp.Error != nil && resp.Error.Error() == "key not found" {
				// Key not found is a valid response, just increment success count
				successCount++
			} else if resp.Error != nil {
				qm.logger.Errorf("Read failed from node %s: %v", resp.NodeID, resp.Error)
			}
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("read operation timed out")
		}
	}

	// Check if we achieved read quorum
	if successCount < qm.config.R {
		return nil, nil, fmt.Errorf("read quorum not achieved: %d/%d nodes responded", successCount, qm.config.R)
	}

	// If we have no valid responses but achieved quorum, the key doesn't exist
	if len(validResponses) == 0 {
		return nil, nil, nil
	}

	// Resolve conflicts using vector clocks
	return qm.resolveReadConflicts(key, validResponses)
}

// resolveReadConflicts resolves conflicts from multiple read responses
func (qm *QuorumManager) resolveReadConflicts(key string, responses []*ReadResponse) ([]byte, *versioning.VectorClock, error) {
	if len(responses) == 0 {
		return nil, nil, fmt.Errorf("no valid responses")
	}

	if len(responses) == 1 {
		return responses[0].Value, responses[0].VClock, nil
	}

	// Create versioned values for conflict resolution
	var versionedValues []*versioning.VersionedValue
	for _, resp := range responses {
		vv := &versioning.VersionedValue{
			Key:         key,
			Value:       resp.Value,
			VectorClock: resp.VClock,
			NodeID:      resp.NodeID,
		}
		versionedValues = append(versionedValues, vv)
	}

	// Create conflict set and resolve
	conflictSet := &versioning.ConflictSet{
		Key:    key,
		Values: versionedValues,
	}

	if conflictSet.HasConflicts() {
		qm.logger.Warnf("Conflict detected for key %s, resolving with strategy", key)
	}

	resolved := conflictSet.Resolve(qm.resolver)
	if resolved == nil {
		return nil, nil, fmt.Errorf("failed to resolve conflicts")
	}

	return resolved.Value, resolved.VectorClock, nil
}

// Delete performs a quorum delete operation
func (qm *QuorumManager) Delete(ctx context.Context, key string) error {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	// Get nodes responsible for this key
	partition, err := qm.partitioner.GetPartitionForKey(key)
	if err != nil {
		return fmt.Errorf("failed to get partition: %w", err)
	}

	// Send delete requests to all replica nodes
	responses := make(chan error, len(partition.Nodes))
	successCount := 0

	for _, nodeID := range partition.Nodes {
		go func(nodeID string) {
			// Handle local node operations directly
			if nodeID == qm.localNodeID && qm.localNode != nil {
				err := qm.localNode.LocalDelete(ctx, key)
				responses <- err
				return
			}

			// Handle remote node operations via client
			client, exists := qm.nodeClients[nodeID]
			if !exists {
				responses <- fmt.Errorf("no client for node %s", nodeID)
				return
			}

			err := client.Delete(ctx, key)
			responses <- err
		}(nodeID)
	}

	// Collect responses
	for i := 0; i < len(partition.Nodes); i++ {
		select {
		case err := <-responses:
			if err == nil {
				successCount++
			} else {
				qm.logger.Errorf("Delete failed: %v", err)
			}
		case <-ctx.Done():
			return fmt.Errorf("delete operation timed out")
		}
	}

	// Check if we achieved write quorum for deletes
	if successCount >= qm.config.W {
		qm.logger.Debugf("Delete quorum achieved for key %s (%d/%d nodes)", key, successCount, qm.config.W)
		return nil
	}

	return fmt.Errorf("delete quorum not achieved: %d/%d nodes responded successfully", successCount, qm.config.W)
}

// GetStats returns quorum manager statistics
func (qm *QuorumManager) GetStats() map[string]interface{} {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	return map[string]interface{}{
		"replication_factor":   qm.config.N,
		"read_quorum":          qm.config.R,
		"write_quorum":         qm.config.W,
		"strongly_consistent":  qm.config.IsStronglyConsistent(),
		"registered_nodes":     len(qm.nodeClients),
		"hinted_handoff_stats": qm.hintedHandoff.GetStats(),
	}
}

// SetConflictResolver sets the conflict resolution strategy
func (qm *QuorumManager) SetConflictResolver(resolver versioning.ConflictResolver) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.resolver = resolver
}

// GetHintedHandoff returns the hinted handoff manager
func (qm *QuorumManager) GetHintedHandoff() *HintedHandoff {
	return qm.hintedHandoff
}
