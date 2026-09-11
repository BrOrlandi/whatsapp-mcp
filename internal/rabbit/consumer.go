package rabbit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

type Consumer struct {
	URL, Queue   string
	Store        *store.Store
	SetConnected func(bool)
	OnPersisted  func(time.Time)
	Logger       *slog.Logger
}

func (c *Consumer) Run(ctx context.Context) error {
	if c.SetConnected == nil {
		c.SetConnected = func(bool) {}
	}
	if c.OnPersisted == nil {
		c.OnPersisted = func(time.Time) {}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.consume(ctx)
		c.SetConnected(false)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.Logger.Warn("RabbitMQ consumer disconnected; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	conn, err := amqp.DialConfig(c.URL, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
	if err != nil {
		return err
	}
	defer conn.Close()
	channel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err := channel.Qos(20, 0, false); err != nil {
		return err
	}
	// Evolution Go creates durable quorum queues. Declaring the same topology lets
	// this consumer start first without changing the producer's queue contract.
	if _, err := channel.QueueDeclare(c.Queue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return err
	}
	deliveries, err := channel.Consume(c.Queue, "whatsapp-mcp", false, false, false, false, nil)
	if err != nil {
		return err
	}
	c.SetConnected(true)
	closed := channel.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-closed:
			if err == nil {
				return errors.New("RabbitMQ channel closed")
			}
			return err
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("RabbitMQ delivery channel closed")
			}
			event, err := events.Decode(delivery.Body)
			if err != nil {
				c.Logger.Warn("rejecting malformed Evolution event", "error", err)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return nackErr
				}
				continue
			}
			if err := c.Store.PersistEvent(ctx, event); err != nil {
				c.Logger.Error("event persistence failed; requeueing", "event_id", event.ID, "error", err)
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					return nackErr
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return err
			}
			c.OnPersisted(event.ReceivedAt)
		}
	}
}
