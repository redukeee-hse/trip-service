package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

const meterName = "github.com/redukeee-hse/avitoService/internal/infra/postgres"

// durationBuckets — корзины гистограммы в секундах (семантические конвенции OTel).
var durationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 10}

// queryTracer инструментирует каждый запрос пула, поэтому репозиторий о
// телеметрии не знает и забыть обернуть запрос невозможно.
//
// Спаны создаёт otelpgx: имя по операции (SELECT, INSERT), атрибуты
// db.operation.name, db.query.text, статус ошибки. Сверху добавляется то,
// чего otelpgx для обычных запросов не пишет: db.collection.name и
// error.type, и метрика db.client.operation.duration с этими лейблами.
// Собственная метрика otelpgx отключена, чтобы не было двух метрик с
// одним именем и разными лейблами.
type queryTracer struct {
	*otelpgx.Tracer // остальные методы (Prepare, Batch, Connect) — как в otelpgx

	duration metric.Float64Histogram
}

type queryInfoKey struct{}

type queryInfo struct {
	start      time.Time
	operation  string
	collection string
}

func newQueryTracer(tp trace.TracerProvider, mp metric.MeterProvider) (*queryTracer, error) {
	duration, err := mp.Meter(meterName).Float64Histogram("db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Длительность запроса в базу данных."),
		metric.WithExplicitBucketBoundaries(durationBuckets...))
	if err != nil {
		return nil, fmt.Errorf("db.client.operation.duration: %w", err)
	}

	return &queryTracer{
		Tracer: otelpgx.NewTracer(
			otelpgx.WithTracerProvider(tp),
			otelpgx.WithMeterProvider(noop.NewMeterProvider()),
			// Спан на ожидание свободного соединения в пуле только шумит:
			// нам нужен ровно один спан на запрос.
			otelpgx.WithDisableAcquireTracer(),
		),
		duration: duration,
	}, nil
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)

	info := queryInfo{start: time.Now()}
	info.operation, info.collection = parseSQL(data.SQL)
	if info.collection != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("db.collection.name", info.collection))
	}
	return context.WithValue(ctx, queryInfoKey{}, info)
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if info, ok := ctx.Value(queryInfoKey{}).(queryInfo); ok {
		attrs := []attribute.KeyValue{
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", info.operation),
		}
		if info.collection != "" {
			attrs = append(attrs, attribute.String("db.collection.name", info.collection))
		}
		if data.Err != nil {
			errType := errorType(data.Err)
			attrs = append(attrs, attribute.String("error.type", errType))
			// Атрибут ставим до вызова otelpgx: он закрывает спан.
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("error.type", errType))
		}
		t.duration.Record(ctx, time.Since(info.start).Seconds(), metric.WithAttributes(attrs...))
	}

	t.Tracer.TraceQueryEnd(ctx, conn, data)
}

// TracePrepareStart — подготовка выражения (pgx кеширует prepared statements
// и готовит запрос при первом выполнении). Это тоже поход в БД со своим
// спаном от otelpgx; добавляем ему db.collection.name, как и запросу.
func (t *queryTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	ctx = t.Tracer.TracePrepareStart(ctx, conn, data)
	if _, collection := parseSQL(data.SQL); collection != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("db.collection.name", collection))
	}
	return ctx
}

// parseSQL достаёт из запроса операцию и таблицу: "SELECT", "trips".
// Значения низкой кардинальности: в лейбл не попадает ни SQL, ни параметры.
// Для BEGIN и COMMIT таблицы нет — collection пустая.
func parseSQL(sql string) (operation, collection string) {
	operation = otelpgx.SQLOperationName(context.Background(), sql)
	fields := strings.Fields(sql)

	var marker string
	switch operation {
	case "SELECT", "DELETE":
		marker = "FROM"
	case "INSERT":
		marker = "INTO"
	case "UPDATE":
		if len(fields) > 1 {
			return operation, cleanTableName(fields[1])
		}
		return operation, ""
	default:
		return operation, ""
	}

	for i := 0; i < len(fields)-1; i++ {
		if strings.EqualFold(fields[i], marker) {
			return operation, cleanTableName(fields[i+1])
		}
	}
	return operation, ""
}

// cleanTableName убирает кавычки и скобку списка колонок: `"trips"(id` → trips.
func cleanTableName(token string) string {
	if i := strings.IndexByte(token, '('); i >= 0 {
		token = token[:i]
	}
	return strings.ToLower(strings.Trim(token, `"`))
}

// errorType — тип ошибки для error.type: SQLSTATE для ошибок Postgres
// (23505 — нарушение уникальности), иначе вид ошибки. Низкая кардинальность:
// текст ошибки сюда не попадает.
func errorType(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		return pgErr.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "_OTHER"
	}
}
