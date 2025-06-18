package partition

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
)

const (
	// DefaultVirtualNodes is the default number of virtual nodes per physical node
	DefaultVirtualNodes = 150
)

// Node represents a physical node in the cluster
type Node struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Weight  int    `json:"weight"` // For heterogeneous clusters
}

// ConsistentHash implements consistent hashing for distributed partitioning
type ConsistentHash struct {
	nodes        map[string]*Node  // nodeID -> Node
	ring         map[uint64]string // hash -> nodeID
	sortedHashes []uint64
	virtualNodes int
	mu           sync.RWMutex
}

// NewConsistentHash creates a new consistent hash ring
func NewConsistentHash(virtualNodes int) *ConsistentHash {
	if virtualNodes <= 0 {
		virtualNodes = DefaultVirtualNodes
	}

	return &ConsistentHash{
		nodes:        make(map[string]*Node),
		ring:         make(map[uint64]string),
		virtualNodes: virtualNodes,
	}
}

// AddNode adds a node to the hash ring
func (ch *ConsistentHash) AddNode(node *Node) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if node.Weight <= 0 {
		node.Weight = 1
	}

	ch.nodes[node.ID] = node

	// Add virtual nodes to the ring
	virtualNodeCount := ch.virtualNodes * node.Weight
	for i := 0; i < virtualNodeCount; i++ {
		virtualKey := fmt.Sprintf("%s:%d", node.ID, i)
		hash := ch.hash(virtualKey)
		ch.ring[hash] = node.ID
	}

	ch.updateSortedHashes()
}

// RemoveNode removes a node from the hash ring
func (ch *ConsistentHash) RemoveNode(nodeID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	node, exists := ch.nodes[nodeID]
	if !exists {
		return
	}

	// Remove virtual nodes from the ring
	virtualNodeCount := ch.virtualNodes * node.Weight
	for i := 0; i < virtualNodeCount; i++ {
		virtualKey := fmt.Sprintf("%s:%d", nodeID, i)
		hash := ch.hash(virtualKey)
		delete(ch.ring, hash)
	}

	delete(ch.nodes, nodeID)
	ch.updateSortedHashes()
}

// GetNode returns the node responsible for a given key
func (ch *ConsistentHash) GetNode(key string) (*Node, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	hash := ch.hash(key)
	idx := ch.findNextNode(hash)
	nodeID := ch.ring[ch.sortedHashes[idx]]
	return ch.nodes[nodeID], nil
}

// GetNodes returns N nodes responsible for a given key (for replication)
func (ch *ConsistentHash) GetNodes(key string, count int) ([]*Node, error) {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.nodes) == 0 {
		return nil, fmt.Errorf("no nodes available")
	}

	if count > len(ch.nodes) {
		count = len(ch.nodes)
	}

	hash := ch.hash(key)
	nodes := make([]*Node, 0, count)
	visited := make(map[string]bool)

	for len(nodes) < count {
		idx := ch.findNextNode(hash)
		nodeID := ch.ring[ch.sortedHashes[idx]]

		if !visited[nodeID] {
			nodes = append(nodes, ch.nodes[nodeID])
			visited[nodeID] = true
		}

		// Move to next position in ring
		if idx == len(ch.sortedHashes)-1 {
			hash = ch.sortedHashes[0]
		} else {
			hash = ch.sortedHashes[idx+1]
		}
	}

	return nodes, nil
}

// GetAllNodes returns all nodes in the cluster
func (ch *ConsistentHash) GetAllNodes() []*Node {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	nodes := make([]*Node, 0, len(ch.nodes))
	for _, node := range ch.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetNodeCount returns the number of nodes in the cluster
func (ch *ConsistentHash) GetNodeCount() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return len(ch.nodes)
}

// hash computes SHA256 hash of the key
func (ch *ConsistentHash) hash(key string) uint64 {
	hasher := sha256.New()
	hasher.Write([]byte(key))
	hashBytes := hasher.Sum(nil)

	// Convert first 8 bytes to uint64
	var hash uint64
	for i := 0; i < 8 && i < len(hashBytes); i++ {
		hash = hash<<8 + uint64(hashBytes[i])
	}
	return hash
}

// findNextNode finds the next node in the ring for a given hash
func (ch *ConsistentHash) findNextNode(hash uint64) int {
	idx := sort.Search(len(ch.sortedHashes), func(i int) bool {
		return ch.sortedHashes[i] >= hash
	})

	if idx == len(ch.sortedHashes) {
		idx = 0 // Wrap around to the beginning
	}

	return idx
}

// updateSortedHashes updates the sorted hash slice
func (ch *ConsistentHash) updateSortedHashes() {
	ch.sortedHashes = make([]uint64, 0, len(ch.ring))
	for hash := range ch.ring {
		ch.sortedHashes = append(ch.sortedHashes, hash)
	}
	sort.Slice(ch.sortedHashes, func(i, j int) bool {
		return ch.sortedHashes[i] < ch.sortedHashes[j]
	})
}

// GetKeyDistribution returns the distribution of keys across nodes
func (ch *ConsistentHash) GetKeyDistribution(keys []string) map[string]int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	distribution := make(map[string]int)
	for _, key := range keys {
		node, err := ch.GetNode(key)
		if err == nil {
			distribution[node.ID]++
		}
	}
	return distribution
}

// RebalanceInfo represents information about data movement during rebalancing
type RebalanceInfo struct {
	KeysMoved      []string       `json:"keys_moved"`
	SourceNodes    map[string]int `json:"source_nodes"`
	TargetNodes    map[string]int `json:"target_nodes"`
	TotalKeysMoved int            `json:"total_keys_moved"`
}

// GetRebalanceInfo returns information about keys that need to be moved
// when nodes are added or removed
func (ch *ConsistentHash) GetRebalanceInfo(keys []string, oldRing *ConsistentHash) *RebalanceInfo {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if oldRing != nil {
		oldRing.mu.RLock()
		defer oldRing.mu.RUnlock()
	}

	info := &RebalanceInfo{
		KeysMoved:   make([]string, 0),
		SourceNodes: make(map[string]int),
		TargetNodes: make(map[string]int),
	}

	for _, key := range keys {
		newNode, err := ch.GetNode(key)
		if err != nil {
			continue
		}

		if oldRing == nil {
			// First time adding keys
			info.TargetNodes[newNode.ID]++
			continue
		}

		oldNode, err := oldRing.GetNode(key)
		if err != nil || oldNode.ID != newNode.ID {
			// Key needs to be moved
			info.KeysMoved = append(info.KeysMoved, key)
			info.TotalKeysMoved++

			if oldNode != nil {
				info.SourceNodes[oldNode.ID]++
			}
			info.TargetNodes[newNode.ID]++
		}
	}

	return info
}

// Stats returns statistics about the hash ring
func (ch *ConsistentHash) Stats() map[string]interface{} {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	virtualNodeCount := 0
	nodeWeights := make(map[string]int)

	for nodeID, node := range ch.nodes {
		virtualNodeCount += ch.virtualNodes * node.Weight
		nodeWeights[nodeID] = node.Weight
	}

	return map[string]interface{}{
		"physical_nodes":   len(ch.nodes),
		"virtual_nodes":    virtualNodeCount,
		"ring_positions":   len(ch.ring),
		"virtual_per_node": ch.virtualNodes,
		"node_weights":     nodeWeights,
	}
}

// Partition represents a data partition with its range and responsible nodes
type Partition struct {
	ID        string   `json:"id"`
	StartHash uint64   `json:"start_hash"`
	EndHash   uint64   `json:"end_hash"`
	Nodes     []string `json:"nodes"` // Primary and replica node IDs
}

// PartitionManager manages partitions and their assignments
type PartitionManager struct {
	partitions        []*Partition
	replicationFactor int
	consistentHash    *ConsistentHash
	mu                sync.RWMutex
}

// NewPartitionManager creates a new partition manager
func NewPartitionManager(replicationFactor int) *PartitionManager {
	return &PartitionManager{
		partitions:        make([]*Partition, 0),
		replicationFactor: replicationFactor,
		consistentHash:    NewConsistentHash(DefaultVirtualNodes),
	}
}

// AddNode adds a node to the partition manager
func (pm *PartitionManager) AddNode(node *Node) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.consistentHash.AddNode(node)
	pm.updatePartitions()
}

// RemoveNode removes a node from the partition manager
func (pm *PartitionManager) RemoveNode(nodeID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.consistentHash.RemoveNode(nodeID)
	pm.updatePartitions()
}

// GetPartitionForKey returns the partition responsible for a key
func (pm *PartitionManager) GetPartitionForKey(key string) (*Partition, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	nodes, err := pm.consistentHash.GetNodes(key, pm.replicationFactor)
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]string, len(nodes))
	for i, node := range nodes {
		nodeIDs[i] = node.ID
	}

	hash := pm.consistentHash.hash(key)

	return &Partition{
		ID:        fmt.Sprintf("partition-%d", hash%1000), // Simple partition ID
		StartHash: hash,
		EndHash:   hash,
		Nodes:     nodeIDs,
	}, nil
}

// updatePartitions updates the partition assignments (simplified implementation)
func (pm *PartitionManager) updatePartitions() {
	// In a real implementation, this would create fixed partitions
	// and assign them to nodes. For simplicity, we're using
	// dynamic partitions based on consistent hashing.
	pm.partitions = make([]*Partition, 0)
}

// GetStats returns partition manager statistics
func (pm *PartitionManager) GetStats() map[string]interface{} {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := pm.consistentHash.Stats()
	stats["replication_factor"] = pm.replicationFactor
	stats["partitions"] = len(pm.partitions)

	return stats
}
