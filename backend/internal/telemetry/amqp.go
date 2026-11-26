package telemetry

import (
	"context"
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const amqpCorrelationHeader = "x-correlation-id"

// InjectTraceContext serializes W3C trace context and correlation_id into a compact JSON string
// suitable for persisting in the Platform DB outbox table alongside the queued execution.
func InjectTraceContext(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if corrID := CorrelationIDFromContext(ctx); corrID != "" {
		carrier["correlation_id"] = corrID
	}
	if len(carrier) == 0 {
		return ""
	}
	raw, err := json.Marshal(carrier)
	if err != nil {
		return ""
	}
	return string(raw)
}

// ExtractTraceContext restores a W3C trace context and correlation_id serialized by InjectTraceContext.
func ExtractTraceContext(ctx context.Context, raw string) context.Context {
	if raw == "" {
		return ctx
	}
	var carrier propagation.MapCarrier
	if err := json.Unmarshal([]byte(raw), &carrier); err != nil {
		return ctx
	}
	if len(carrier) == 0 {
		return ctx
	}
	if corrID := carrier["correlation_id"]; corrID != "" {
		ctx = WithCorrelationID(ctx, corrID)
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectAMQPHeaders injects W3C trace headers and x-correlation-id into an amqp.Table.
func InjectAMQPHeaders(ctx context.Context, headers amqp.Table) amqp.Table {
	if headers == nil {
		headers = amqp.Table{}
	}
	otel.GetTextMapPropagator().Inject(ctx, amqpTableCarrier(headers))
	if corrID := CorrelationIDFromContext(ctx); corrID != "" {
		headers[amqpCorrelationHeader] = corrID
	}
	return headers
}

// ExtractAMQPHeaders extracts W3C trace headers and x-correlation-id from an amqp.Table into ctx.
func ExtractAMQPHeaders(ctx context.Context, headers amqp.Table) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	if corrID := amqpTableCarrier(headers).Get(amqpCorrelationHeader); corrID != "" {
		ctx = WithCorrelationID(ctx, corrID)
	}
	return otel.GetTextMapPropagator().Extract(ctx, amqpTableCarrier(headers))
}

type amqpTableCarrier amqp.Table

func (c amqpTableCarrier) Get(key string) string {
	raw, ok := c[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func (c amqpTableCarrier) Set(key, val string) {
	c[key] = val
}

func (c amqpTableCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
