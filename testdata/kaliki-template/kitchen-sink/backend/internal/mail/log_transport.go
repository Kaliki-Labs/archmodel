package mail

import (
	"context"
	"log"
)

// logTransport writes the email to the server log instead of sending it. It is
// the default when no real provider is configured. Credentials are never placed
// in API responses, so this keeps them observable during local development.
type logTransport struct{}

func (logTransport) deliver(_ context.Context, to, subject, htmlBody string) error {
	log.Printf("[mail] to=%s subject=%q\n%s", to, subject, htmlBody)
	return nil
}
