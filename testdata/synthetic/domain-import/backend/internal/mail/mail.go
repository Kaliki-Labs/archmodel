// Package mail sends mail. It wrongly imports a Domain.
package mail

import "example.com/synth/backend/internal/items"

// Mailer sends mail.
type Mailer struct{ I *items.Service }
