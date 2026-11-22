package config

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
)

// Протоколы OTLP, которые поддерживает сервис (значения из спецификации OTel).
const (
	ProtocolGRPC         = "grpc"
	ProtocolHTTPProtobuf = "http/protobuf"
)

// samplers — допустимые значения OTEL_TRACES_SAMPLER (спецификация OTel).
var samplers = []string{
	"always_on",
	"always_off",
	"traceidratio",
	"parentbased_always_on",
	"parentbased_always_off",
	"parentbased_traceidratio",
}

// TelemetryConfig — настройки OpenTelemetry. Имена переменных стандартные,
// их же понимают коллектор и любые OTel-инструменты.
type TelemetryConfig struct {
	ServiceName string  // OTEL_SERVICE_NAME
	Endpoint    string  // OTEL_EXPORTER_OTLP_ENDPOINT, например http://localhost:4318
	Protocol    string  // OTEL_EXPORTER_OTLP_PROTOCOL: grpc или http/protobuf
	Sampler     string  // OTEL_TRACES_SAMPLER
	SamplerArg  float64 // OTEL_TRACES_SAMPLER_ARG: доля для *traceidratio, 0..1
}

func loadTelemetry() (TelemetryConfig, error) {
	cfg := TelemetryConfig{
		ServiceName: getStringEnv("OTEL_SERVICE_NAME", "trip-service"),
		Endpoint:    getStringEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"),
		Protocol:    getStringEnv("OTEL_EXPORTER_OTLP_PROTOCOL", ProtocolHTTPProtobuf),
		Sampler:     getStringEnv("OTEL_TRACES_SAMPLER", "parentbased_always_on"),
		SamplerArg:  1,
	}

	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return TelemetryConfig{}, fmt.Errorf(
			"OTEL_EXPORTER_OTLP_ENDPOINT должен быть URL вида http://host:port, получено %q", cfg.Endpoint,
		)
	}

	if cfg.Protocol != ProtocolGRPC && cfg.Protocol != ProtocolHTTPProtobuf {
		return TelemetryConfig{}, fmt.Errorf(
			"OTEL_EXPORTER_OTLP_PROTOCOL должен быть %s или %s, получено %q",
			ProtocolGRPC, ProtocolHTTPProtobuf, cfg.Protocol,
		)
	}

	if !slices.Contains(samplers, cfg.Sampler) {
		return TelemetryConfig{}, fmt.Errorf("OTEL_TRACES_SAMPLER должен быть одним из %v, получено %q", samplers, cfg.Sampler)
	}

	if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
		arg, err := strconv.ParseFloat(raw, 64)
		if err != nil || arg < 0 || arg > 1 {
			return TelemetryConfig{}, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG должен быть числом от 0 до 1, получено %q", raw)
		}
		cfg.SamplerArg = arg
	}

	return cfg, nil
}
