package telemetry

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultServiceVersion = "dev"
	defaultEnvironment    = "development"
	defaultMetricsAddr    = ":9091"
	defaultSampleRatio    = 1.0
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
	serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	serviceVersion := strings.TrimSpace(os.Getenv("OTEL_SERVICE_VERSION"))
	if serviceVersion == "" {
		serviceVersion = defaultServiceVersion
	}

	environment := strings.TrimSpace(os.Getenv("OTEL_ENVIRONMENT"))
	if environment == "" {
		environment = strings.TrimSpace(os.Getenv("APP_ENV"))
	}
	if environment == "" {
		environment = defaultEnvironment
	}

	otlpEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	tracesExporter := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER")))
	if tracesExporter != "" {
		for _, part := range strings.Split(tracesExporter, ",") {
			token := strings.TrimSpace(part)
			switch token {
			case "otlp", "console", "none":
			default:
				return Config{}, fmt.Errorf("OTEL_TRACES_EXPORTER contains unsupported exporter %q", token)
			}
		}
	}

	sampleRatio := defaultSampleRatio
	if rawRatio := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG")); rawRatio != "" {
		parsed, err := strconv.ParseFloat(rawRatio, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return Config{}, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be a float between 0 and 1: %q", rawRatio)
		}
		sampleRatio = parsed
	}

	metricsAddr := strings.TrimSpace(os.Getenv("APP_METRICS_ADDR"))
	if metricsAddr == "" {
		metricsAddr = defaultMetricsAddr
	}

	return Config{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Environment:    environment,
		OTLPEndpoint:   otlpEndpoint,
		TracesExporter: tracesExporter,
		SampleRatio:    sampleRatio,
		MetricsAddr:    metricsAddr,
	}, nil
}

// parseOTLPEndpoint normalizes an OTLP endpoint into host:port and TLS insecurity mode.
func parseOTLPEndpoint(raw string) (hostPort string, insecure bool) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimRight(trimmed, "/")

	switch {
	case strings.HasPrefix(trimmed, "https://"):
		return strings.TrimPrefix(trimmed, "https://"), false
	case strings.HasPrefix(trimmed, "http://"):
		return strings.TrimPrefix(trimmed, "http://"), true
	default:
		return trimmed, true
	}
}
