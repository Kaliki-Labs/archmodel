// Package auth signs users in.
package auth

import "example.com/synth/backend/internal/config"

// Service is the auth service.
type Service struct{ C config.Config }

// ListAuth lists auth things.
func (s *Service) ListAuth() {}
