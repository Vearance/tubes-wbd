package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

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

// Provider coordinates OpenTelemetry tracing, logging, and Prometheus metrics exposition.
type Provider struct {
	cfg            Config
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
		expOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
		if insecure {
			expOpts = append(expOpts, otlptracehttp.WithInsecure())
		}
		traceExp, err := otlptracehttp.New(ctx, expOpts...)
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
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	stdoutHandler := slog.NewJSONHandler(os.Stdout, nil)
	handlers := []slog.Handler{stdoutHandler}

	var lp *sdklog.LoggerProvider
	if cfg.OTLPEndpoint != "" {
		endpoint, insecure := parseOTLPEndpoint(cfg.OTLPEndpoint)
		logOpts := []otlploghttp.Option{otlploghttp.WithEndpoint(endpoint)}
		if insecure {
			logOpts = append(logOpts, otlploghttp.WithInsecure())
		}
		logExp, err := otlploghttp.New(ctx, logOpts...)
		if err != nil {
			_ = tp.Shutdown(ctx)
			return nil, fmt.Errorf("creating OTLP log exporter: %w", err)
		}
		lp = sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		)
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

	promExp, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registry),
		otelprometheus.WithoutScopeInfo(),
	)
	if err != nil {
		_ = shutdownProviders(ctx, tp, lp, nil)
		return nil, fmt.Errorf("creating Prometheus metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promExp),
	)
	otel.SetMeterProvider(mp)

	meter := mp.Meter("tubes-wbd")
	appMetrics, err := newAppMetrics(meter)
	if err != nil {
		_ = shutdownProviders(ctx, tp, lp, mp)
		return nil, fmt.Errorf("creating application metric instruments: %w", err)
	}
	activeMetrics.Store(appMetrics)

	var baseHandler slog.Handler
	if len(handlers) == 1 {
		baseHandler = handlers[0]
	} else {
		baseHandler = NewMultiLogHandler(handlers...)
	}
	logHandler := NewTraceLogHandler(baseHandler, cfg.ServiceName)

	return &Provider{
		cfg:            cfg,
		tracerProvider: tp,
		meterProvider:  mp,
		loggerProvider: lp,
		registry:       registry,
		meter:          meter,
		metrics:        appMetrics,
		logHandler:     logHandler,
	}, nil
}

// MetricsHandler returns the Prometheus HTTP scrape handler for this provider.
func (p *Provider) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{})
}

// LogHandler returns the configured structured slog.Handler (JSON stdout + OTLP when enabled).
func (p *Provider) LogHandler() slog.Handler {
	return p.logHandler
}

// Meter returns the OpenTelemetry Meter bound to the provider's Prometheus exporter.
func (p *Provider) Meter() metric.Meter {
	return p.meter
}

// RegisterMigrationStatusCollector registers observable gauges for migration job counts per
// bounded status and pending outbox items. Call only on the API server to avoid double-counting.
func (p *Provider) RegisterMigrationStatusCollector(
	fn func(context.Context) (statusCounts map[string]int64, outboxPending int64, err error),
) error {
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

	outboxGauge, err := p.meter.Int64ObservableGauge(
		"bscheme_migration_outbox_pending",
		metric.WithDescription("Current number of unpublished migration execution outbox records"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_migration_outbox_pending gauge: %w", err)
	}

	_, err = p.meter.RegisterCallback(func(ctx context.Context, obs metric.Observer) error {
		counts, pending, err := fn(ctx)
		if err != nil {
			slog.WarnContext(ctx, "collecting migration status gauges", "error", err)
			return nil
		}

		for _, status := range KnownMigrationStatuses {
			obs.ObserveInt64(jobsGauge, counts[status], metric.WithAttributes(attribute.String("status", status)))
		}
		obs.ObserveInt64(outboxGauge, pending)
		return nil
	}, jobsGauge, outboxGauge)
	if err != nil {
		return fmt.Errorf("registering migration status callback: %w", err)
	}
	return nil
}

// Shutdown flushes and closes trace, log, and metric providers.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	activeMetrics.CompareAndSwap(p.metrics, nil)
	return shutdownProviders(ctx, p.tracerProvider, p.loggerProvider, p.meterProvider)
}

func shutdownProviders(
	ctx context.Context,
	tp *sdktrace.TracerProvider,
	lp *sdklog.LoggerProvider,
	mp *sdkmetric.MeterProvider,
) error {
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
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

func parseTraceExporters(raw string, hasOTLPEndpoint bool) (enableOTLP, enableConsole bool) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return hasOTLPEndpoint, false
	}
	for _, part := range strings.Split(trimmed, ",") {
		switch strings.TrimSpace(part) {
		case "otlp":
			if hasOTLPEndpoint {
				enableOTLP = true
			}
		case "console":
			enableConsole = true
		case "none":
			return false, false
		}
	}
	return enableOTLP, enableConsole
}
