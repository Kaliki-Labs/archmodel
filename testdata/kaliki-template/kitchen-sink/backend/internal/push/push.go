// Package push sends push notifications via Firebase Cloud Messaging. This is an
// integration point: build your notification payloads on top of Send.
package push

import (
	"context"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"

	"github.com/example/kitchen-sink-app/backend/internal/config"
)

type Service struct {
	client *messaging.Client
}

func New(ctx context.Context, cfg config.FirebaseConfig) (*Service, error) {
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(cfg.CredentialsFile))
	if err != nil {
		return nil, err
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, err
	}
	return &Service{client: client}, nil
}

// Send delivers a notification to a single device token.
func (s *Service) Send(ctx context.Context, deviceToken, title, body string) (string, error) {
	return s.client.Send(ctx, &messaging.Message{
		Token:        deviceToken,
		Notification: &messaging.Notification{Title: title, Body: body},
	})
}
