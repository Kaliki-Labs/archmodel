// Command workers runs background jobs. It currently hosts the transactional
// outbox relay: it publishes unpublished `outbox` rows to Kafka/Redpanda for
// at-least-once delivery, then marks them published.
//
// Domain code enqueues events with the EnqueueOutbox query in the SAME database
// transaction as the domain change; this relay delivers them out-of-band.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/kitchen-sink-app/backend/internal/config"
	"github.com/example/kitchen-sink-app/backend/internal/database"
	"github.com/example/kitchen-sink-app/backend/internal/events"
	"github.com/example/kitchen-sink-app/backend/internal/observability"
)

const (
	batchSize  = 100
	idlePeriod = 2 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config/env.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	obs := observability.Init(cfg.Observability.ServiceName+"-workers", cfg.Observability.Endpoint)
	defer obs.Shutdown()

	poolCfg, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer(otelpgx.WithTracerProvider(obs.TracerProvider()))
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	publisher := events.NewPublisher(cfg.Kafka)
	defer publisher.Close()

	q := database.New(pool)
	log.Println("outbox relay started")

	for {
		n, err := relayOnce(ctx, q, publisher)
		if err != nil {
			log.Printf("outbox relay: %v", err)
		}
		// Only idle when there was nothing to do; otherwise drain quickly.
		if n == 0 {
			select {
			case <-ctx.Done():
				log.Println("outbox relay stopped")
				return
			case <-time.After(idlePeriod):
			}
		} else if ctx.Err() != nil {
			log.Println("outbox relay stopped")
			return
		}
	}
}

// relayOnce publishes one batch and returns how many rows it delivered.
func relayOnce(ctx context.Context, q *database.Queries, publisher *events.Publisher) (int, error) {
	rows, err := q.ListUnpublishedOutbox(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	for i, row := range rows {
		if err := publisher.Publish(ctx, row.Topic, row.Key, row.Payload); err != nil {
			// Stop the batch; unpublished rows are retried on the next tick.
			return i, err
		}
		if err := q.MarkOutboxPublished(ctx, row.ID); err != nil {
			return i, err
		}
	}
	return len(rows), nil
}
