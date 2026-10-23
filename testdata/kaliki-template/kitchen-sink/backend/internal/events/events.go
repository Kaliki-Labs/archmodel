// Package events publishes domain events to Kafka/Redpanda. Services call
// Publisher.Publish; swap in the transactional outbox pattern for at-least-once
// delivery (see TEMPLATE_NOTES.md).
package events

import (
	"context"

	"github.com/segmentio/kafka-go"

	"github.com/example/kitchen-sink-app/backend/internal/config"
)

type Publisher struct {
	writer *kafka.Writer
}

func NewPublisher(cfg config.KafkaConfig) *Publisher {
	return &Publisher{
		writer: &kafka.Writer{
			Addr:                   kafka.TCP(cfg.Brokers...),
			Balancer:               &kafka.LeastBytes{},
			AllowAutoTopicCreation: true,
		},
	}
}

// Publish writes a single message to the given topic.
func (p *Publisher) Publish(ctx context.Context, topic string, key, value []byte) error {
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   key,
		Value: value,
	})
}

func (p *Publisher) Close() error { return p.writer.Close() }
