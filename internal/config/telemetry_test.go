package config

import (
	"strings"
	"testing"
)

const (
	envServiceName = "OTEL_SERVICE_NAME"
	envEndpoint    = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envProtocol    = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envSampler     = "OTEL_TRACES_SAMPLER"
	envSamplerArg  = "OTEL_TRACES_SAMPLER_ARG"
)

// Тесты меняют переменные окружения через t.Setenv, поэтому без t.Parallel:
// окружение процесса общее.

func TestLoadTelemetry_Defaults(t *testing.T) {
	for _, name := range []string{
		envServiceName, envEndpoint, envProtocol,
		envSampler, envSamplerArg,
	} {
		t.Setenv(name, "")
	}

	got, err := loadTelemetry()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := TelemetryConfig{
		ServiceName: "trip-service",
		Endpoint:    "http://localhost:4318",
		Protocol:    ProtocolHTTPProtobuf,
		Sampler:     "parentbased_always_on",
		SamplerArg:  1,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadTelemetry_FromEnv(t *testing.T) {
	t.Setenv(envServiceName, "trip-service-test")
	t.Setenv(envEndpoint, "http://collector:4317")
	t.Setenv(envProtocol, ProtocolGRPC)
	t.Setenv(envSampler, "parentbased_traceidratio")
	t.Setenv(envSamplerArg, "0.25")

	got, err := loadTelemetry()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := TelemetryConfig{
		ServiceName: "trip-service-test",
		Endpoint:    "http://collector:4317",
		Protocol:    ProtocolGRPC,
		Sampler:     "parentbased_traceidratio",
		SamplerArg:  0.25,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Кривое значение должно остановить запуск с понятной ошибкой, а не
// молча выключить телеметрию.
func TestLoadTelemetry_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "endpoint without scheme", env: map[string]string{envEndpoint: "localhost:4318"}, wantErr: envEndpoint},
		{name: "unknown protocol", env: map[string]string{envProtocol: "http/json"}, wantErr: envProtocol},
		{name: "unknown sampler", env: map[string]string{envSampler: "sometimes"}, wantErr: envSampler},
		{name: "ratio above 1", env: map[string]string{envSamplerArg: "1.5"}, wantErr: envSamplerArg},
		{name: "ratio not a number", env: map[string]string{envSamplerArg: "half"}, wantErr: envSamplerArg},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{
				envEndpoint, envProtocol,
				envSampler, envSamplerArg,
			} {
				t.Setenv(name, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			_, err := loadTelemetry()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want mention of %s", err, tt.wantErr)
			}
		})
	}
}
