package server

import (
	"strings"
)

// pathAllowed reports whether a path is inside one of the configured sharing roots.
// An empty list disables the allowlist for backwards compatibility.
func (s *Server) pathAllowed(p string) bool {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return true
	}
	for _, root := range s.cfg.AllowedPaths {
		if p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

// pathNavigable permits listing an ancestor only when it leads to an allowed root.
// The handler still filters the listing, so this does not expose sibling content.
func (s *Server) pathNavigable(p string) bool {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return true
	}
	if s.pathAllowed(p) {
		return true
	}
	prefix := strings.TrimSuffix(p, "/") + "/"
	for _, root := range s.cfg.AllowedPaths {
		if strings.HasPrefix(root, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) pathVisible(p string) bool {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return true
	}
	if s.pathAllowed(p) {
		return true
	}
	prefix := strings.TrimSuffix(p, "/") + "/"
	for _, root := range s.cfg.AllowedPaths {
		if strings.HasPrefix(root, prefix) {
			return true
		}
	}
	return false
}
