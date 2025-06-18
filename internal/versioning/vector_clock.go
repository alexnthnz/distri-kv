package versioning

import (
	"encoding/json"
	"fmt"
	"time"
)

// VectorClock represents a vector clock for distributed versioning
type VectorClock struct {
	Clocks    map[string]int64 `json:"clocks"`
	Timestamp time.Time        `json:"timestamp"`
}

// NewVectorClock creates a new vector clock
func NewVectorClock() *VectorClock {
	return &VectorClock{
		Clocks:    make(map[string]int64),
		Timestamp: time.Now(),
	}
}

// Increment increments the clock for a given node
func (vc *VectorClock) Increment(nodeID string) {
	vc.Clocks[nodeID]++
	vc.Timestamp = time.Now()
}

// Update updates the vector clock with another vector clock
func (vc *VectorClock) Update(other *VectorClock) {
	for nodeID, clock := range other.Clocks {
		if vc.Clocks[nodeID] < clock {
			vc.Clocks[nodeID] = clock
		}
	}
	vc.Timestamp = time.Now()
}

// Compare compares two vector clocks and returns the relationship
type Relationship int

const (
	Before     Relationship = iota // This vector clock is before the other
	After                          // This vector clock is after the other
	Concurrent                     // Vector clocks are concurrent (conflict)
	Equal                          // Vector clocks are equal
)

// Compare compares this vector clock with another
func (vc *VectorClock) Compare(other *VectorClock) Relationship {
	if vc.Equal(other) {
		return Equal
	}

	thisGreater := false
	otherGreater := false

	// Get all node IDs from both clocks
	allNodes := make(map[string]bool)
	for nodeID := range vc.Clocks {
		allNodes[nodeID] = true
	}
	for nodeID := range other.Clocks {
		allNodes[nodeID] = true
	}

	// Compare each node's clock
	for nodeID := range allNodes {
		thisValue := vc.Clocks[nodeID]
		otherValue := other.Clocks[nodeID]

		if thisValue > otherValue {
			thisGreater = true
		} else if thisValue < otherValue {
			otherGreater = true
		}
	}

	if thisGreater && !otherGreater {
		return After
	} else if !thisGreater && otherGreater {
		return Before
	} else {
		return Concurrent
	}
}

// Equal checks if two vector clocks are equal
func (vc *VectorClock) Equal(other *VectorClock) bool {
	if len(vc.Clocks) != len(other.Clocks) {
		// Check if missing entries are all zeros
		allNodes := make(map[string]bool)
		for nodeID := range vc.Clocks {
			allNodes[nodeID] = true
		}
		for nodeID := range other.Clocks {
			allNodes[nodeID] = true
		}

		for nodeID := range allNodes {
			if vc.Clocks[nodeID] != other.Clocks[nodeID] {
				return false
			}
		}
		return true
	}

	for nodeID, clock := range vc.Clocks {
		if other.Clocks[nodeID] != clock {
			return false
		}
	}
	return true
}

// Clone creates a deep copy of the vector clock
func (vc *VectorClock) Clone() *VectorClock {
	clone := &VectorClock{
		Clocks:    make(map[string]int64),
		Timestamp: vc.Timestamp,
	}
	for nodeID, clock := range vc.Clocks {
		clone.Clocks[nodeID] = clock
	}
	return clone
}

// String returns a string representation of the vector clock
func (vc *VectorClock) String() string {
	data, _ := json.Marshal(vc)
	return string(data)
}

// FromString creates a vector clock from a string representation
func FromString(s string) (*VectorClock, error) {
	var vc VectorClock
	if err := json.Unmarshal([]byte(s), &vc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vector clock: %w", err)
	}
	return &vc, nil
}

// VersionedValue represents a value with its vector clock
type VersionedValue struct {
	Key         string       `json:"key"`
	Value       []byte       `json:"value"`
	VectorClock *VectorClock `json:"vector_clock"`
	NodeID      string       `json:"node_id"`
}

// NewVersionedValue creates a new versioned value
func NewVersionedValue(key string, value []byte, nodeID string) *VersionedValue {
	vc := NewVectorClock()
	vc.Increment(nodeID)

	return &VersionedValue{
		Key:         key,
		Value:       value,
		VectorClock: vc,
		NodeID:      nodeID,
	}
}

// Update updates the versioned value with a new vector clock
func (vv *VersionedValue) Update(other *VersionedValue) {
	vv.VectorClock.Update(other.VectorClock)
	vv.VectorClock.Increment(vv.NodeID)
}

// IsConflictWith checks if this value conflicts with another
func (vv *VersionedValue) IsConflictWith(other *VersionedValue) bool {
	return vv.VectorClock.Compare(other.VectorClock) == Concurrent
}

// ConflictResolver handles conflict resolution strategies
type ConflictResolver interface {
	Resolve(values []*VersionedValue) *VersionedValue
}

// LastWriteWinsResolver resolves conflicts using last write wins strategy
type LastWriteWinsResolver struct{}

func (r *LastWriteWinsResolver) Resolve(values []*VersionedValue) *VersionedValue {
	if len(values) == 0 {
		return nil
	}

	latest := values[0]
	for _, value := range values[1:] {
		if value.VectorClock.Timestamp.After(latest.VectorClock.Timestamp) {
			latest = value
		}
	}
	return latest
}

// ClientSideResolver lets the client handle conflict resolution
type ClientSideResolver struct{}

func (r *ClientSideResolver) Resolve(values []*VersionedValue) *VersionedValue {
	// Return the first value but client should handle conflicts
	if len(values) > 0 {
		return values[0]
	}
	return nil
}

// ConflictSet represents a set of conflicting values
type ConflictSet struct {
	Key    string            `json:"key"`
	Values []*VersionedValue `json:"values"`
}

// HasConflicts checks if there are any conflicts in the set
func (cs *ConflictSet) HasConflicts() bool {
	if len(cs.Values) <= 1 {
		return false
	}

	for i := 0; i < len(cs.Values); i++ {
		for j := i + 1; j < len(cs.Values); j++ {
			if cs.Values[i].IsConflictWith(cs.Values[j]) {
				return true
			}
		}
	}
	return false
}

// Resolve resolves conflicts using the provided resolver
func (cs *ConflictSet) Resolve(resolver ConflictResolver) *VersionedValue {
	if !cs.HasConflicts() && len(cs.Values) > 0 {
		// No conflicts, return the latest version
		latest := cs.Values[0]
		for _, value := range cs.Values[1:] {
			if value.VectorClock.Compare(latest.VectorClock) == After {
				latest = value
			}
		}
		return latest
	}

	return resolver.Resolve(cs.Values)
}
