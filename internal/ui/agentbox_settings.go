package ui

import "errors"

func (s *Settings) workspaceMode() string {
	if s == nil {
		return "developer"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mode == "agentbox" {
		return s.Mode
	}
	return "developer"
}

func (s *Settings) SetMode(mode string) error {
	if mode != "developer" && mode != "agentbox" {
		return errors.New("use /mode developer or /mode agentbox")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.Mode
	s.Mode = mode
	if err := s.save(); err != nil {
		s.Mode = previous
		return err
	}
	return nil
}
