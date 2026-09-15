// Package rabbit consumes every queue Evolution publishes to.
//
// Evolution declares one durable quorum queue per event in global mode, and a
// queue nobody consumes grows without bound. Consuming all of them is therefore
// both how the gateway learns what happened and how the broker stays healthy.
package rabbit

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// Queues are the queues Evolution creates for the events this gateway
// subscribes to. The names come from Evolution's own global-queue mapping:
// MESSAGE, SEND_MESSAGE, HISTORY_SYNC and CONNECTION.
var Queues = []string{
	"message",
	"sendmessage",
	"historysync",
	"connected",
	"disconnected",
	"loggedout",
	"pairsuccess",
	"connectfailure",
	"temporaryban",
}

type Consumer struct {
	URL    string
	Queues []string
	Store  *store.Store
	State  *health.State
	Logger *slog.Logger
}

func (c *Consumer) Run(ctx context.Context) error {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if len(c.Queues) == 0 {
		c.Queues = Queues
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.session(ctx)
		c.State.SetRabbit(false)
		for _, queue := range c.Queues {
			c.State.SetQueueConsuming(queue, false, errorText(err))
		}
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

// session holds one connection and one consumer per queue. Any queue failing
// tears the whole session down, because a partially consumed set silently
// stops recording part of what happened.
func (c *Consumer) session(ctx context.Context) error {
	conn, err := amqp.DialConfig(c.URL, amqp.Config{Heartbeat: 10 * time.Second, Dial: amqp.DefaultDial(10 * time.Second)})
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	failures := make(chan error, len(c.Queues))
	var wg sync.WaitGroup
	for _, queue := range c.Queues {
		channel, err := conn.Channel()
		if err != nil {
			return err
		}
		defer channel.Close()
		if err := channel.Qos(20, 0, false); err != nil {
			return err
		}
		// Evolution creates durable quorum queues. Declaring the same topology lets
		// this consumer start first without changing the producer's contract.
		if _, err := channel.QueueDeclare(queue, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
			return err
		}
		deliveries, err := channel.Consume(queue, "whatsapp-mcp", false, false, false, false, nil)
		if err != nil {
			return err
		}
		c.State.SetQueueConsuming(queue, true, "")
		closed := channel.NotifyClose(make(chan *amqp.Error, 1))
		wg.Add(1)
		go func(queue string, deliveries <-chan amqp.Delivery, closed chan *amqp.Error) {
			defer wg.Done()
			failures <- c.drain(ctx, queue, deliveries, closed)
		}(queue, deliveries, closed)
	}
	c.State.SetRabbit(true)

	select {
	case err := <-failures:
		cancel()
		wg.Wait()
		return err
	case <-ctx.Done():
		wg.Wait()
		return ctx.Err()
	}
}

func (c *Consumer) drain(ctx context.Context, queue string, deliveries <-chan amqp.Delivery, closed chan *amqp.Error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-closed:
			if err == nil {
				return errors.New("RabbitMQ channel closed for queue " + queue)
			}
			return err
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("RabbitMQ delivery channel closed for queue " + queue)
			}
			if err := c.handle(ctx, queue, delivery); err != nil {
				return err
			}
		}
	}
}

// handle persists one delivery and acknowledges it only after the transaction
// commits. Malformed payloads are dropped without requeue so one bad message
// cannot loop forever; a database failure is requeued, because it will succeed
// once the database is back.
func (c *Consumer) handle(ctx context.Context, queue string, delivery amqp.Delivery) error {
	decoded, err := events.Decode(delivery.Body)
	if err != nil {
		c.Logger.Warn("rejecting malformed Evolution event", "queue", queue, "error", err)
		c.State.MarkQueueEvent(queue, time.Now(), true)
		return delivery.Nack(false, false)
	}
	if err := c.Store.PersistEvent(ctx, decoded.Record); err != nil {
		c.Logger.Error("event persistence failed; requeueing", "queue", queue, "event_id", decoded.Record.ID, "error", err)
		c.State.MarkQueueEvent(queue, time.Now(), true)
		return delivery.Nack(false, true)
	}
	if err := delivery.Ack(false); err != nil {
		return err
	}
	c.observe(queue, decoded)
	return nil
}

// observe folds a persisted event into the live status the panel and the MCP
// tools read.
func (c *Consumer) observe(queue string, decoded events.Event) {
	at := decoded.Record.ReceivedAt
	c.State.MarkQueueEvent(queue, at, false)
	c.State.MarkEvent(at)
	switch decoded.Kind {
	case events.KindMessage:
		c.State.MarkMessage(at)
	case events.KindHistory:
		c.State.MarkHistory(at)
		c.Logger.Info("history sync ingested", "messages", len(decoded.Record.Messages))
	case events.KindConnection:
		if decoded.Connection == nil {
			return
		}
		update := decoded.Connection
		c.State.SetWhatsApp(string(update.State), update.Reason, update.JID, update.PushName)
		c.Logger.Info("WhatsApp connection changed", "state", update.State, "reason", update.Reason)
	}
}

func errorText(err error) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return ""
	}
	return err.Error()
}
