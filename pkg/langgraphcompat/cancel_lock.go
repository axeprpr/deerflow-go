package langgraphcompat

import "strings"

func (s *Server) lockDetachedCancel(runID string) func() {
	if s == nil {
		return func() {}
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return func() {}
	}

	s.detachedCancelMu.Lock()
	if s.detachedCancels == nil {
		s.detachedCancels = make(map[string]*detachedCancelEntry)
	}
	entry := s.detachedCancels[runID]
	if entry == nil {
		entry = &detachedCancelEntry{}
		s.detachedCancels[runID] = entry
	}
	entry.refs++
	s.detachedCancelMu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.detachedCancelMu.Lock()
		entry.refs--
		if entry.refs <= 0 {
			delete(s.detachedCancels, runID)
		}
		s.detachedCancelMu.Unlock()
	}
}
