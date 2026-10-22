package mail

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"github.com/example/kitchen-sink-app/backend/internal/config"
)

// sesTransport sends via AWS SES v2. Credentials resolve from the static config
// values if set, otherwise from the default AWS chain (env, profile, IAM role).
type sesTransport struct {
	client *sesv2.Client
	from   string
}

func newSESTransport(cfg config.MailConfig) sesTransport {
	loadOpts := make([]func(*awsconfig.LoadOptions) error, 0, 2)
	if cfg.SES.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.SES.Region))
	}
	if cfg.SES.AccessKeyID != "" && cfg.SES.SecretAccessKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.SES.AccessKeyID, cfg.SES.SecretAccessKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		// Return a transport that surfaces the error on first send rather than
		// failing process startup.
		return sesTransport{from: senderHeader(cfg)}
	}
	return sesTransport{client: sesv2.NewFromConfig(awsCfg), from: senderHeader(cfg)}
}

func (t sesTransport) deliver(ctx context.Context, to, subject, htmlBody string) error {
	if t.client == nil {
		return fmt.Errorf("ses: client not initialized")
	}
	_, err := t.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(t.from),
		Destination:      &sesv2types.Destination{ToAddresses: []string{to}},
		Content: &sesv2types.EmailContent{Simple: &sesv2types.Message{
			Subject: &sesv2types.Content{Data: aws.String(subject), Charset: aws.String("UTF-8")},
			Body: &sesv2types.Body{Html: &sesv2types.Content{
				Data: aws.String(htmlBody), Charset: aws.String("UTF-8"),
			}},
		}},
	})
	if err != nil {
		return fmt.Errorf("ses send: %w", err)
	}
	return nil
}
