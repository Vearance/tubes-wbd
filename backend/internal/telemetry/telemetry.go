package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Config holds OpenTelemetry and Prometheus configuration for a BScheme service.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	TracesExporter string
	SampleRatio    float64
	MetricsAddr    string
}

// LoadConfig reads telemetry configuration from standard OTEL_* and APP_* environment variables.
func LoadConfig(defaultServiceName string) (Config, error) {
	serviceName := envOrDefault("OTEL_SERVICE_NAME", defaultServiceName)
	serviceVersion := envOrDefault("OTEL_SERVICE_VERSION", "dev")
	environment := envOrDefault("OTEL_ENVIRONMENT", envOrDefault("APP_ENV", "development"))
	otlpEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))

	tracesExporter := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")))
	if tracesExporter != "" {
		for _, part := range strings.Split(tracesExporter, ",") {
			switch strings.TrimSpace(part) {
			case "otlp", "console", "none":
			default:
				return Config{}, fmt.Errorf("OTEL_TRACES_EXPORTER contains unsupported exporter %q", strings.TrimSpace(part))
			}
		}
	}

	sampleRatio := 1.0
	if raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return Config{}, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be a float between 0 and 1: %q", raw)
		}
		sampleRatio = parsed
	}

	return Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    environment,
		OTLPEndpoint:   otlpEndpoint,
		TracesExporter: tracesExporter,
		SampleRatio:    sampleRatio,
		MetricsAddr:    envOrDefault("APP_METRICS_ADDR", ":9091"),
	}, nil
}

func envOrDefault(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func parseOTLPEndpoint(raw string) (hostPort string, insecure bool) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	switch {
	case strings.HasPrefix(trimmed, "https://"):
		return strings.TrimPrefix(trimmed, "https://"), false
	case strings.HasPrefix(trimmed, "http://"):
		return strings.TrimPrefix(trimmed, "http://"), true
	default:
		return trimmed, true
	}
}

// Provider coordinates OpenTelemetry tracing, logging, and Prometheus metrics exposition.
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *sdklog.LoggerProvider
	registry       *prometheus.Registry
	meter          metric.Meter
	metrics        *AppMetrics
	logHandler     slog.Handler
}

// Init configures global OpenTelemetry trace/log/metric providers and builds a
// dedicated Prometheus registry with Go and Process runtime collectors.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentName(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating telemetry resource: %w", err)
	}

	enableOTLPTraces, enableConsoleTraces := parseTraceExporters(cfg.TracesExporter, cfg.OTLPEndpoint != "")
	traceOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	}
	if enableOTLPTraces {
		endpoint, insecure := parseOTLPEndpoint(cfg.OTLPEndpoint)
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		traceExp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
		}
		traceOpts = append(traceOpts, sdktrace.WithBatcher(traceExp))
	}
	if enableConsoleTraces {
		traceOpts = append(traceOpts, sdktrace.WithSyncer(NewConsoleSpanExporter(os.Stderr)))
	}

	tp := sdktrace.NewTracerProvider(traceOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	handlers := []slog.Handler{slog.NewJSONHandler(os.Stdout, nil)}
	var lp *sdklog.LoggerProvider
	if cfg.OTLPEndpoint != "" {
		endpoint, insecure := parseOTLPEndpoint(cfg.OTLPEndpoint)
		opts := []otlploghttp.Option{otlploghttp.WithEndpoint(endpoint)}
		if insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		logExp, err := otlploghttp.New(ctx, opts...)
		if err != nil {
			_ = tp.Shutdown(ctx)
			return nil, fmt.Errorf("creating OTLP log exporter: %w", err)
		}
		lp = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)))
		otel.SetLoggerProvider(lp)
		handlers = append(handlers, otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp)))
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		_ = shutdownProviders(ctx, tp, lp, nil)
		return nil, fmt.Errorf("registering Go runtime collector: %w", err)
	}
	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		_ = shutdownProviders(ctx, tp, lp, nil)
		return nil, fmt.Errorf("registering Process runtime collector: %w", err)
	}

	promExp, err := otelprometheus.New(otelprometheus.WithRegisterer(registry), otelprometheus.WithoutScopeInfo())
	if err != nil {
		_ = shutdownProviders(ctx, tp, lp, nil)
		return nil, fmt.Errorf("creating Prometheus metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(promExp))
	otel.SetMeterProvider(mp)

	meter := mp.Meter("tubes-wbd")
	appMetrics, err := newAppMetrics(meter)
	if err != nil {
		_ = shutdownProviders(ctx, tp, lp, mp)
		return nil, fmt.Errorf("creating application metric instruments: %w", err)
	}
	activeMetrics.Store(appMetrics)

	baseHandler := handlers[0]
	if len(handlers) > 1 {
		baseHandler = NewMultiLogHandler(handlers...)
	}

	return &Provider{
		tracerProvider: tp,
		meterProvider:  mp,
		loggerProvider: lp,
		registry:       registry,
		meter:          meter,
		metrics:        appMetrics,
		logHandler:     NewTraceLogHandler(baseHandler, cfg.ServiceName),
	}, nil
}

// MetricsHandler returns the Prometheus HTTP scrape handler for this provider.
func (p *Provider) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{})
}

// LogHandler returns the configured structured slog.Handler (JSON stdout + OTLP when enabled).
func (p *Provider) LogHandler() slog.Handler { return p.logHandler }

// Meter returns the OpenTelemetry Meter bound to the provider's Prometheus exporter.
func (p *Provider) Meter() metric.Meter { return p.meter }

// RegisterMigrationStatusCollector registers the bscheme_migration_jobs observable gauge.
func (p *Provider) RegisterMigrationStatusCollector(fn func(context.Context) (map[string]int64, error)) error {
	if fn == nil {
		return errors.New("migration status collector callback is nil")
	}
	jobsGauge, err := p.meter.Int64ObservableGauge(
		"bscheme_migration_jobs",
		metric.WithDescription("Current number of migration executions per status"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_migration_jobs gauge: %w", err)
	}

	_, err = p.meter.RegisterCallback(func(ctx context.Context, obs metric.Observer) error {
		counts, err := fn(ctx)
		if err != nil {
			slog.WarnContext(ctx, "collecting migration status gauges", "error", err)
			return nil
		}
		for _, status := range KnownMigrationStatuses {
			obs.ObserveInt64(jobsGauge, counts[status], metric.WithAttributes(attribute.String("status", status)))
		}
		return nil
	}, jobsGauge)
	return err
}

// MigrationProgressTotals holds database-backed cumulative batch and row counts per execution type.
type MigrationProgressTotals struct {
	Batches int64
	Rows    int64
}

// RegisterMigrationProgressCollector registers observable gauges backed by persisted
// migration_executions batch_count and rows_processed totals so counts reflect real
// database history across worker restarts.
func (p *Provider) RegisterMigrationProgressCollector(fn func(context.Context) (map[string]MigrationProgressTotals, error)) error {
	if fn == nil {
		return errors.New("migration progress collector callback is nil")
	}
	batchesGauge, err := p.meter.Int64ObservableGauge(
		"bscheme_migration_batches_committed",
		metric.WithDescription("Total committed migration batches persisted in platform database by execution type"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_migration_batches_committed gauge: %w", err)
	}
	rowsGauge, err := p.meter.Int64ObservableGauge(
		"bscheme_migration_rows_committed",
		metric.WithDescription("Total committed migration rows processed persisted in platform database by execution type"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_migration_rows_committed gauge: %w", err)
	}

	_, err = p.meter.RegisterCallback(func(ctx context.Context, obs metric.Observer) error {
		totals, err := fn(ctx)
		if err != nil {
			slog.WarnContext(ctx, "collecting migration progress gauges", "error", err)
			return nil
		}
		for _, execType := range []string{"up", "rollback", "forward"} {
			entry := totals[execType]
			attr := metric.WithAttributes(attribute.String("type", execType))
			obs.ObserveInt64(batchesGauge, entry.Batches, attr)
			obs.ObserveInt64(rowsGauge, entry.Rows, attr)
		}
		return nil
	}, batchesGauge, rowsGauge)
	return err
}

// Shutdown flushes and closes trace, log, and metric providers.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	activeMetrics.CompareAndSwap(p.metrics, nil)
	return shutdownProviders(ctx, p.tracerProvider, p.loggerProvider, p.meterProvider)
}

func shutdownProviders(ctx context.Context, tp *sdktrace.TracerProvider, lp *sdklog.LoggerProvider, mp *sdkmetric.MeterProvider) error {
	var errs error
	if tp != nil {
		errs = errors.Join(errs, tp.Shutdown(ctx))
	}
	if lp != nil {
		errs = errors.Join(errs, lp.Shutdown(ctx))
	}
	if mp != nil {
		errs = errors.Join(errs, mp.Shutdown(ctx))
	}
	return errs
}

// Tracer returns an instrumentation tracer from the global TracerProvider.
func Tracer(name string) trace.Tracer { return otel.Tracer(name) }

func parseTraceExporters(raw string, hasOTLPEndpoint bool) (enableOTLP, enableConsole bool) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return hasOTLPEndpoint, false
	}
	for _, part := range strings.Split(trimmed, ",") {
		switch strings.TrimSpace(part) {
		case "otlp":
			enableOTLP = hasOTLPEndpoint
		case "console":
			enableConsole = true
		case "none":
			return false, false
		}
	}
	return enableOTLP, enableConsole
}

// ConsoleSpanExporter writes completed OpenTelemetry spans as scrubbed single-line JSON.
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
	Type         string         `json:"type"`
	Service      string         `json:"service,omitempty"`
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	StartTime    string         `json:"start_time"`
	EndTime      string         `json:"end_time"`
	DurationMs   float64        `json:"duration_ms"`
	StatusCode   string         `json:"status_code"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

// ExportSpans serializes each span to single-line JSON with sensitive fields scrubbed.
func (e *ConsoleSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if err := ctx.Err(); err != nil || len(spans) == 0 {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, span := range spans {
		sc := span.SpanContext()
		parentID := ""
		if p := span.Parent(); p.IsValid() {
			parentID = p.SpanID().String()
		}
		service := ""
		if res := span.Resource(); res != nil {
			for _, kv := range res.Attributes() {
				if kv.Key == semconv.ServiceNameKey {
					service = kv.Value.AsString()
					break
				}
			}
		}
		attrs := make(map[string]any, len(span.Attributes()))
		for _, kv := range span.Attributes() {
			k := string(kv.Key)
			if IsSensitiveLogKey(k) {
				attrs[k] = "[REDACTED]"
			} else if s, ok := kv.Value.AsInterface().(string); ok {
				attrs[k] = ScrubString(s)
			} else {
				attrs[k] = kv.Value.AsInterface()
			}
		}
		line, err := json.Marshal(consoleSpanRecord{
			Type:         "otel_span",
			Service:      service,
			TraceID:      sc.TraceID().String(),
			SpanID:       sc.SpanID().String(),
			ParentSpanID: parentID,
			Name:         span.Name(),
			Kind:         span.SpanKind().String(),
			StartTime:    span.StartTime().UTC().Format(time.RFC3339Nano),
			EndTime:      span.EndTime().UTC().Format(time.RFC3339Nano),
			DurationMs:   RoundMillis(span.EndTime().Sub(span.StartTime())),
			StatusCode:   span.Status().Code.String(),
			Attributes:   attrs,
		})
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
func (e *ConsoleSpanExporter) Shutdown(context.Context) error { return nil }

// RoundMillis formats a duration in milliseconds rounded to three decimal places.
func RoundMillis(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return math.Round(float64(d.Microseconds())) / 1000.0
}
