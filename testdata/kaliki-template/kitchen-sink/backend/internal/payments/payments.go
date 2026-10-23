// Package payments wraps the Razorpay client. This is an integration point:
// add order creation, webhook verification, and subscription handling for your
// product (see TEMPLATE_NOTES.md).
package payments

import (
	razorpay "github.com/razorpay/razorpay-go"

	"github.com/example/kitchen-sink-app/backend/internal/config"
)

type Service struct {
	client *razorpay.Client
}

func New(cfg config.RazorpayConfig) *Service {
	return &Service{client: razorpay.NewClient(cfg.KeyID, cfg.KeySecret)}
}

// Client exposes the underlying Razorpay client for now. Wrap the specific
// operations you need (orders, payments, subscriptions) behind methods here.
func (s *Service) Client() *razorpay.Client { return s.client }
