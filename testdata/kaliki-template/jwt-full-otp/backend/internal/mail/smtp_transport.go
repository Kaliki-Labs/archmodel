package mail

import (
	"context"
	"fmt"
	"net/smtp"

	"github.com/example/jwt-full-otp-app/backend/internal/config"
)

// smtpTransport sends via a standard SMTP server. Locally this points at
// Mailpit (no auth); in other environments set username/password.
type smtpTransport struct {
	addr     string
	auth     smtp.Auth
	fromAddr string // envelope sender (bare address)
	fromHdr  string // From: header (may include display name)
}

func newSMTPTransport(cfg config.MailConfig) smtpTransport {
	var auth smtp.Auth
	if cfg.SMTP.Username != "" {
		auth = smtp.PlainAuth("", cfg.SMTP.Username, cfg.SMTP.Password, cfg.SMTP.Host)
	}
	return smtpTransport{
		addr:     fmt.Sprintf("%s:%d", cfg.SMTP.Host, cfg.SMTP.Port),
		auth:     auth,
		fromAddr: cfg.SenderAddress,
		fromHdr:  senderHeader(cfg),
	}
}

func (t smtpTransport) deliver(_ context.Context, to, subject, htmlBody string) error {
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n"+
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		t.fromHdr, to, subject, htmlBody)
	if err := smtp.SendMail(t.addr, t.auth, t.fromAddr, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
