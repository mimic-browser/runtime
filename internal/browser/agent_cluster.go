package browser

import (
	"strings"
	"sync"
)

// Agent-cluster choices belong to a document tree's browsing context group.
// Retained documents retain this state; a new top-level navigation starts a
// fresh group. Removed child frames do not erase choices for their origins.
type agentClusterState struct {
	mu      sync.RWMutex
	origins map[string]bool
}

func originAgentClusterForResponse(secure bool, header string) bool {
	return secure && strings.TrimSpace(header) != "?0"
}

func (r *Realm) initializeAgentCluster(header string) {
	frame, isFrame := r.agent.(*Frame)
	child := isFrame && frame.parent != nil && frame.parent.Realm != nil
	if child {
		r.agentClusters = frame.parent.Realm.agentClusters
	}
	if r.agentClusters == nil {
		r.agentClusters = &agentClusterState{origins: make(map[string]bool)}
	}
	if _, exists := r.agentClusters.choice(r.origin); exists {
		return
	}
	security := r.securityState()
	choice := security.originAgentCluster
	if child && r.url.Scheme != "about" {
		choice = originAgentClusterForResponse(security.secureContext, header)
	}
	r.agentClusters.remember(r.origin, choice)
}

func (s *agentClusterState) choice(origin string) (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, exists := s.origins[origin]
	return value, exists
}
func (s *agentClusterState) remember(origin string, choice bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.origins[origin]; !exists {
		s.origins[origin] = choice
	}
}
