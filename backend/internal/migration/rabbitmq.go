package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"
)

const defaultExecutionQueue = "migration.executions"

type executionMessage struct {
	ExecutionID uint `json:"execution_id"`
}

func DeclareExecutionQueue(channel *amqp.Channel, queue string) error {
	if channel == nil {
		return errors.New("migration rabbitmq: channel is nil")
	}
	if queue == "" {
		queue = defaultExecutionQueue
	}
	_, err := channel.QueueDeclare(queue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("declaring migration execution queue: %w", err)
	}
	return nil
}

type OutboxRelay struct {
	db       *gorm.DB
	channel  *amqp.Channel
	queue    string
	interval time.Duration
}

func NewOutboxRelay(db *gorm.DB, channel *amqp.Channel, queue string) (*OutboxRelay, error) {
	if db == nil || channel == nil {
		return nil, errors.New("migration outbox relay: db and channel are required")
	}
	if queue == "" {
		queue = defaultExecutionQueue
	}
	return &OutboxRelay{db: db, channel: channel, queue: queue, interval: time.Second}, nil
}

// Run publishes committed outbox rows. The message contains only the execution
// ID; the immutable execution snapshot remains in the Platform DB.
func (r *OutboxRelay) Run(ctx context.Context) error {
	confirms := r.channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := r.channel.Confirm(false); err != nil {
		return fmt.Errorf("enabling RabbitMQ publisher confirms: %w", err)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if err := r.publishBatch(ctx, confirms); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOutboxRelay reconnects to RabbitMQ and resumes publishing pending outbox rows.
func RunOutboxRelay(ctx context.Context, db *gorm.DB, rabbitURL, queue string) error {
	return runRabbitMQ(ctx, rabbitURL, queue, "outbox relay", func(channel *amqp.Channel) error {
		relay, err := NewOutboxRelay(db, channel, queue)
		if err != nil {
			return err
		}
		return relay.Run(ctx)
	})
}

// ConsumeWithReconnect resumes consuming after RabbitMQ closes the connection.
// Unacked deliveries are requeued by RabbitMQ when their channel closes.
func (w *ExecutionWorker) ConsumeWithReconnect(ctx context.Context, rabbitURL, queue string) error {
	return runRabbitMQ(ctx, rabbitURL, queue, "migration worker", func(channel *amqp.Channel) error {
		if err := DeclareExecutionQueue(channel, queue); err != nil {
			return err
		}
		return w.Consume(ctx, channel, queue)
	})
}

func runRabbitMQ(ctx context.Context, rabbitURL, queue, component string, run func(*amqp.Channel) error) error {
	delay := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		connection, err := amqp.Dial(rabbitURL)
		if err == nil {
			var channel *amqp.Channel
			channel, err = connection.Channel()
			if err == nil {
				err = DeclareExecutionQueue(channel, queue)
				if err == nil {
					err = run(channel)
				}
				_ = channel.Close()
			}
			_ = connection.Close()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		slog.Warn("RabbitMQ connection lost; retrying", "component", component, "error", err, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func (r *OutboxRelay) publishBatch(ctx context.Context, confirms <-chan amqp.Confirmation) error {
	var messages []ExecutionOutbox
	if err := r.db.WithContext(ctx).
		Where("published_at IS NULL AND available_at <= ?", time.Now()).
		Order("id").Limit(50).Find(&messages).Error; err != nil {
		return fmt.Errorf("loading migration outbox: %w", err)
	}
	for _, message := range messages {
		body, err := json.Marshal(executionMessage{ExecutionID: message.ExecutionID})
		if err != nil {
			return err
		}
		if err := r.channel.PublishWithContext(ctx, "", r.queue, false, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    fmt.Sprint(message.ID),
			Timestamp:    time.Now(),
			Body:         body,
		}); err != nil {
			return fmt.Errorf("publishing migration execution %d: %w", message.ExecutionID, err)
		}
		select {
		case confirmation, ok := <-confirms:
			if !ok || !confirmation.Ack {
				return fmt.Errorf("RabbitMQ did not confirm outbox message %d", message.ID)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		publishedAt := time.Now()
		if err := r.db.WithContext(ctx).Model(&ExecutionOutbox{}).
			Where("id = ? AND published_at IS NULL", message.ID).
			Update("published_at", publishedAt).Error; err != nil {
			return fmt.Errorf("marking migration outbox message published: %w", err)
		}
	}
	return nil
}

func (w *ExecutionWorker) Consume(ctx context.Context, channel *amqp.Channel, queue string) error {
	if channel == nil {
		return errors.New("migration worker: RabbitMQ channel is nil")
	}
	if queue == "" {
		queue = defaultExecutionQueue
	}
	if err := DeclareExecutionQueue(channel, queue); err != nil {
		return err
	}
	if err := channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("setting migration worker prefetch: %w", err)
	}
	deliveries, err := channel.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consuming migration executions: %w", err)
	}
	recoveryTicker := time.NewTicker(30 * time.Second)
	defer recoveryTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-recoveryTicker.C:
			if err := w.RecoverStale(ctx); err != nil {
				return fmt.Errorf("recovering stale migration executions: %w", err)
			}
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("migration worker: RabbitMQ delivery channel closed")
			}
			var message executionMessage
			if err := json.Unmarshal(delivery.Body, &message); err != nil || message.ExecutionID == 0 {
				if rejectErr := delivery.Reject(false); rejectErr != nil {
					return fmt.Errorf("rejecting invalid migration message: %w", rejectErr)
				}
				continue
			}
			if err := w.RunExecution(ctx, message.ExecutionID); err != nil {
				delay := time.Second
				if errors.Is(err, ErrExecutionBusy) {
					delay = 2 * time.Second
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				if nackErr := delivery.Nack(false, true); nackErr != nil {
					return errors.Join(err, nackErr)
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("acking migration execution %d: %w", message.ExecutionID, err)
			}
		}
	}
}
