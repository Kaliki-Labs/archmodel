// Package server wires everything together.
package server

import (
	"example.com/synth/backend/internal/auth"
	"example.com/synth/backend/internal/items"
	"example.com/synth/backend/internal/mail"
)

// New wires the modules.
func New() (*auth.Service, *items.Service, *mail.Mailer) { return nil, nil, nil }
