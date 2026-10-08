package cdp

// networkConnectionID projects the transport's opaque identity into CDP's
// numeric namespace. The namespace belongs to the server, so different Pages
// and attached clients observe the same ID for a reused transport connection.
// No network connection (for example a synthetic response) is represented by 0.
func (s *Server) networkConnectionID(identity string) uint64 {
	if identity == "" {
		return 0
	}
	s.networkIDMu.Lock()
	defer s.networkIDMu.Unlock()
	if s.networkIDs == nil {
		s.networkIDs = make(map[string]uint64)
	}
	if id := s.networkIDs[identity]; id != 0 {
		return id
	}
	id := uint64(len(s.networkIDs)) + 1
	s.networkIDs[identity] = id
	return id
}
