// Package storage wraps an S3-compatible object store. This is an integration
// point: use PresignPut/PresignGet to hand clients time-limited upload/download
// URLs. Follow the ownership-prefix convention (users/<id>/..., etc.).
package storage

import (
	"context"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	appconfig "github.com/example/kitchen-sink-app/backend/internal/config"
)

type Service struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func New(ctx context.Context, cfg appconfig.StorageConfig) (*Service, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = &cfg.Endpoint
			o.UsePathStyle = true
		}
	})
	return &Service{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  cfg.Bucket,
	}, nil
}

// PresignPut returns a time-limited URL a client can PUT an object to.
func (s *Service) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}
