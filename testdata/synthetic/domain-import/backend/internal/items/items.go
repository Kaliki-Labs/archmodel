// Package items manages items. It wrongly imports another Domain.
package items

import "example.com/synth/backend/internal/auth"

// Service is the items service.
type Service struct{ A *auth.Service }

// ListItems lists items.
func (s *Service) ListItems() {}
