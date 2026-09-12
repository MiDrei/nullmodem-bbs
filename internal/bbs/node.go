package bbs

import (
	"sync"
	"time"
)

// NodeInfo describes one active session for status displays ("who's
// online") and, eventually, the web admin node monitor.
type NodeInfo struct {
	Node      int
	RemoteIP  string
	TermType  string
	Username  string
	Connected time.Time
}

// NodeManager tracks all currently connected sessions. It is safe for
// concurrent use.
type NodeManager struct {
	mu       sync.Mutex
	nodes    map[int]*NodeInfo
	nextNode int
}

// NewNodeManager returns an empty node registry.
func NewNodeManager() *NodeManager {
	return &NodeManager{nodes: make(map[int]*NodeInfo), nextNode: 1}
}

// Join registers a new session and returns its allocated node number.
func (m *NodeManager) Join(remoteIP, termType string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := m.nextNode
	m.nextNode++
	m.nodes[n] = &NodeInfo{
		Node:      n,
		RemoteIP:  remoteIP,
		TermType:  termType,
		Username:  "(logging in)",
		Connected: time.Now(),
	}
	return n
}

// SetUsername updates the username shown for a node once a session
// authenticates.
func (m *NodeManager) SetUsername(node int, username string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.nodes[node]; ok {
		info.Username = username
	}
}

// Leave removes a node from the registry when its session ends.
func (m *NodeManager) Leave(node int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes, node)
}

// Snapshot returns a copy of all currently active nodes, ordered by
// node number, safe to read without further locking.
func (m *NodeManager) Snapshot() []NodeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]NodeInfo, 0, len(m.nodes))
	for _, info := range m.nodes {
		out = append(out, *info)
	}
	sortNodes(out)
	return out
}

func sortNodes(nodes []NodeInfo) {
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j].Node < nodes[j-1].Node; j-- {
			nodes[j], nodes[j-1] = nodes[j-1], nodes[j]
		}
	}
}
