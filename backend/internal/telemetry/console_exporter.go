package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// ConsoleSpanExporter writes completed OpenTelemetry spans as scrubbed single-line
// JSON objects (defaulting to os.Stderr) so traces can be inspected directly from
// console output without requiring an external trace backend.
type ConsoleSpanExporter struct {
	mu  sync.Mutex
	out io.Writer
}

// NewConsoleSpanExporter creates a ConsoleSpanExporter writing to out (os.Stderr if nil).
func NewConsoleSpanExporter(out io.Writer) *ConsoleSpanExporter {
	if out == nil {
		out = os.Stderr
	}
	return &ConsoleSpanExporter{out: out}
}

type consoleSpanRecord struct {
	Type          string               `json:"type"`
	Service       string               `json:"service,omitempty"`
	TraceID       string               `json:"trace_id"`
	SpanID        string               `json:"span_id"`
	ParentSpanID  string               `json:"parent_span_id,omitempty"`
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	StartTime     string               `json:"start_time"`
	EndTime       string               `json:"end_time"`
	DurationMs    float64              `json:"duration_ms"`
	StatusCode    string               `json:"status_code"`
	StatusMessage string               `json:"status_message,omitempty"`
	Attributes    map[string]any       `json:"attributes,omitempty"`
	Events        []consoleSpanEvent   `json:"events,omitempty"`
}

type consoleSpanEvent struct {
	Name       string         `json:"name"`
	Timestamp  string         `json:"timestamp"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// ExportSpans serializes each span to single-line JSON with sensitive fields scrubbed.
func (e *ConsoleSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(spans) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, span := range spans {
		rec := buildConsoleSpanRecord(span)
		line, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if _, err := e.out.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown implements sdktrace.SpanExporter.
func (e *ConsoleSpanExporter) Shutdown(context.Context) error {
	return nil
}

func buildConsoleSpanRecord(span sdktrace.ReadOnlySpan) consoleSpanRecord {
	sc := span.SpanContext()
	parent := span.Parent()
	parentSpanID := ""
	if parent.IsValid() {
		parentSpanID = parent.SpanID().String()
	}

	serviceName := ""
	if res := span.Resource(); res != nil {
		for _, kv := range res.Attributes() {
			if kv.Key == semconv.ServiceNameKey {
				serviceName = kv.Value.AsString()
				break
			}
		}
	}

	status := span.Status()
	rec := consoleSpanRecord{
		Type:          "otel_span",
		Service:       serviceName,
		TraceID:       sc.TraceID().String(),
		SpanID:        sc.SpanID().String(),
		ParentSpanID:  parentSpanID,
		Name:          span.Name(),
		Kind:          span.SpanKind().String(),
		StartTime:     span.StartTime().UTC().Format(time.RFC3339Nano),
		EndTime:       span.EndTime().UTC().Format(time.RFC3339Nano),
		DurationMs:    RoundMillis(span.EndTime().Sub(span.StartTime())),
		StatusCode:    status.Code.String(),
		StatusMessage: ScrubString(status.Description),
		Attributes:    scrubSpanAttributes(span.Attributes()),
	}

	if events := span.Events(); len(events) > 0 {
		rec.Events = make([]consoleSpanEvent, 0, len(events))
		for _, ev := range events {
			rec.Events = append(rec.Events, consoleSpanEvent{
				Name:       ev.Name,
				Timestamp:  ev.Time.UTC().Format(time.RFC3339Nano),
				Attributes: scrubSpanAttributes(ev.Attributes),
			})
		}
	}
	return rec
}

func scrubSpanAttributes(attrs []attribute.KeyValue) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for _, kv := range attrs {
		key := string(kv.Key)
		if IsSensitiveLogKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		val := kv.Value.AsInterface()
		if s, ok := val.(string); ok {
			if looksLikeSecretValue(s) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = ScrubString(s)
			}
			continue
		}
		out[key] = val
	}
	return out
}

// RoundMillis formats a duration in milliseconds rounded to three decimal places.
func RoundMillis(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return math.Round(float64(d.Microseconds())) / 1000.0
}
