package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/redukeee-hse/avitoService/internal/config"
)

// Providers — всё, что нужно инструментированному коду. Провайдеры
// передаются зависимостями (в otelhttp, otelpgx, счётчики поездок), а не
// берутся из глобальных otel.GetTracerProvider/GetMeterProvider.
type Providers struct {
	Tracer     *sdktrace.TracerProvider
	Meter      *sdkmetric.MeterProvider
	Propagator propagation.TextMapPropagator
}

// Setup поднимает OpenTelemetry SDK: ресурс сервиса, экспорт трейсов и
// метрик по OTLP в коллектор, сэмплер и пропагатор W3C trace context.
//
// Экспорт асинхронный: SDK копит спаны и метрики в памяти и отправляет
// пачками в фоне. Если коллектор недоступен, отправка падает в фоне, ошибка
// уходит в лог как Warn, а запросы обслуживаются как обычно.
func Setup(ctx context.Context, cfg config.TelemetryConfig, log *slog.Logger) (*Providers, error) {
	res, err := newResource(ctx, cfg.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}

	traceExporter, err := newTraceExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("trace exporter: %w", err)
	}
	metricExporter, err := newMetricExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("metric exporter: %w", err)
	}

	sampler, err := newSampler(cfg.Sampler, cfg.SamplerArg)
	if err != nil {
		return nil, err
	}

	// Обработчик ошибок у SDK один на процесс, другого способа узнать о
	// неудачном экспорте нет. Отказ коллектора — деградация, с которой
	// сервис справился сам, поэтому Warn, а не Error.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Warn("telemetry export failed", slog.String("error", err.Error()))
	}))

	return &Providers{
		Tracer: sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
			sdktrace.WithBatcher(traceExporter),
		),
		// Интервал отправки метрик берётся из OTEL_METRIC_EXPORT_INTERVAL
		// (по умолчанию 60 секунд) — SDK читает переменную сам.
		Meter: sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		),
		Propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	}, nil
}

// Shutdown дописывает накопленные спаны и метрики и останавливает экспорт.
func (p *Providers) Shutdown(ctx context.Context) error {
	return errors.Join(p.Tracer.Shutdown(ctx), p.Meter.Shutdown(ctx))
}

// newResource описывает сервис: эти атрибуты получат все спаны и метрики.
// Значения по умолчанию из docs/observability.md; OTEL_RESOURCE_ATTRIBUTES
// из окружения их переопределяет (опции применяются по порядку).
func newResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", buildVersion()),
			attribute.String("service.namespace", "tripgo"),
			attribute.String("deployment.environment", "local"),
		),
		resource.WithFromEnv(),
	)
}

// buildVersion — версия модуля, которую Go вшивает при сборке из git
// (тег или псевдоверсия с хешем коммита).
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func newTraceExporter(ctx context.Context, cfg config.TelemetryConfig) (sdktrace.SpanExporter, error) {
	if cfg.Protocol == config.ProtocolGRPC {
		return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
	}
	return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(signalURL(cfg.Endpoint, "traces")))
}

func newMetricExporter(ctx context.Context, cfg config.TelemetryConfig) (sdkmetric.Exporter, error) {
	if cfg.Protocol == config.ProtocolGRPC {
		return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(cfg.Endpoint))
	}
	return otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(signalURL(cfg.Endpoint, "metrics")))
}

// signalURL — адрес приёма сигнала по OTLP/HTTP: к базовому
// OTEL_EXPORTER_OTLP_ENDPOINT добавляется /v1/traces или /v1/metrics.
func signalURL(endpoint, signal string) string {
	return strings.TrimRight(endpoint, "/") + "/v1/" + signal
}

// newSampler переводит OTEL_TRACES_SAMPLER в сэмплер SDK. parentbased_*
// следует решению вызывающего сервиса, если тот прислал traceparent.
func newSampler(name string, ratio float64) (sdktrace.Sampler, error) {
	switch name {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio), nil
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	default:
		return nil, fmt.Errorf("unknown sampler %q", name)
	}
}
