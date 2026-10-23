// Package mail renders and delivers transactional emails (verification and
// password reset).
//
// SECURITY: credentials (verification tokens, reset
// tokens) are delivered here — they are NEVER returned in API
// responses. The transport is selected at runtime from MailConfig.Provider;
// an unknown/empty provider falls back to the log transport, which prints the
// message to the server log so credentials stay observable in development.
package mail

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"time"

	"github.com/example/kitchen-sink-app/backend/internal/config"
)

//go:embed templates/*.html
var templateFS embed.FS

// Mailer sends transactional emails.
type Mailer interface {
	SendVerification(ctx context.Context, to, credential string) error
	SendPasswordReset(ctx context.Context, to, credential string) error
}

// transport delivers a fully-rendered message. Providers implement this.
type transport interface {
	deliver(ctx context.Context, to, subject, htmlBody string) error
}

type templatedMailer struct {
	t    transport
	tmpl *template.Template
	cfg  config.MailConfig
}

type templateData struct {
	AppName string
	Year    int
	Code    string
	Link    string
}

// New selects a transport from cfg.Provider and returns a Mailer.
func New(cfg config.MailConfig) Mailer {
	return &templatedMailer{
		t:    pickTransport(cfg),
		tmpl: template.Must(template.ParseFS(templateFS, "templates/*.html")),
		cfg:  cfg,
	}
}

func pickTransport(cfg config.MailConfig) transport {
	switch cfg.Provider {
	case "smtp":
		return newSMTPTransport(cfg)
	case "ses":
		return newSESTransport(cfg)
	default:
		return logTransport{}
	}
}

func (m *templatedMailer) SendVerification(ctx context.Context, to, credential string) error {
	return m.send(ctx, to, m.subject("Verify your email"), "verification.html",
		m.data(credential, "/verify"))
}

func (m *templatedMailer) SendPasswordReset(ctx context.Context, to, credential string) error {
	return m.send(ctx, to, m.subject("Reset your password"), "password_reset.html",
		m.data(credential, "/reset-password"))
}

func (m *templatedMailer) data(credential, path string) templateData {
	d := templateData{AppName: m.appName(), Year: time.Now().Year()}
	d.Link = m.cfg.BaseURL + path + "?token=" + credential
	return d
}

func (m *templatedMailer) send(ctx context.Context, to, subject, tmplName string, data templateData) error {
	var body bytes.Buffer
	if err := m.tmpl.ExecuteTemplate(&body, tmplName, data); err != nil {
		return fmt.Errorf("render %s: %w", tmplName, err)
	}
	return m.t.deliver(ctx, to, subject, body.String())
}

func (m *templatedMailer) appName() string {
	if m.cfg.AppName != "" {
		return m.cfg.AppName
	}
	return "Kitchen Sink App"
}

func (m *templatedMailer) subject(s string) string {
	if m.cfg.AppName != "" {
		return m.cfg.AppName + ": " + s
	}
	return s
}

// senderHeader formats the From header ("Name <addr>" or bare address).
func senderHeader(cfg config.MailConfig) string {
	if cfg.SenderName != "" {
		return fmt.Sprintf("%s <%s>", cfg.SenderName, cfg.SenderAddress)
	}
	return cfg.SenderAddress
}
