package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/redukeee-hse/avitoService/internal/gateway/push"
)

func newClient(t *testing.T, url string, timeout time.Duration) *push.Client {
	t.Helper()
	c := push.New(url, timeout, noop.NewTracerProvider(), propagation.TraceContext{})
	t.Cleanup(c.Close)
	return c
}

// Запрос соответствует контракту Push Service, 202 — успех.
func TestRequestPosition_Accepted(t *testing.T) {
	t.Parallel()

	tripID, driverID := uuid.New(), uuid.New()
	var got struct {
		method, path, contentType string
		body                      map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"message_id":"m-1","accepted_at":"2026-11-27T10:00:00Z"}`))
	}))
	t.Cleanup(srv.Close)

	// Слэш на конце адреса не должен дать //api/v1/push.
	if err := newClient(t, srv.URL+"/", time.Second).RequestPosition(context.Background(), tripID, driverID); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	if got.method != http.MethodPost || got.path != "/api/v1/push" {
		t.Errorf("запрос %s %s, want POST /api/v1/push", got.method, got.path)
	}
	if got.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.contentType)
	}
	if got.body["recipient_id"] != driverID.String() {
		t.Errorf("recipient_id = %v, want driver_id %s", got.body["recipient_id"], driverID)
	}
	if got.body["kind"] != "REQUEST_POSITION" {
		t.Errorf("kind = %v, want REQUEST_POSITION", got.body["kind"])
	}
	if id, _ := got.body["request_id"].(string); id == "" {
		t.Error("request_id пустой")
	}
	data, _ := got.body["data"].(map[string]any)
	if data["trip_id"] != tripID.String() {
		t.Errorf("data.trip_id = %v, want %s", data["trip_id"], tripID)
	}
}

// Коды ответа различаются: 4xx — наша ошибка, 5xx/429 — соседа.
func TestRequestPosition_Statuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   error // nil — «ни Rejected, ни Unavailable, но ошибка»
	}{
		{http.StatusBadRequest, push.ErrRejected},
		{http.StatusNotFound, push.ErrRejected},
		{http.StatusTooManyRequests, push.ErrUnavailable}, // 429 — тоже 4xx, но временная
		{http.StatusInternalServerError, push.ErrUnavailable},
		{http.StatusServiceUnavailable, push.ErrUnavailable},
		{http.StatusOK, nil}, // контракт обещает 202, 200 — нарушение
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)

			err := newClient(t, srv.URL, time.Second).RequestPosition(context.Background(), uuid.New(), uuid.New())
			if err == nil {
				t.Fatalf("status %d: err = nil, want ошибку", tt.status)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("status %d: err = %v, want errors.Is %v", tt.status, err, tt.want)
			}
			if tt.want == nil && (errors.Is(err, push.ErrRejected) || errors.Is(err, push.ErrUnavailable)) {
				t.Errorf("status %d: err = %v, want нарушение контракта, а не Rejected/Unavailable", tt.status, err)
			}
			if !strings.Contains(err.Error(), http.StatusText(tt.status)) && !strings.Contains(err.Error(), itoa(tt.status)) {
				t.Errorf("в ошибке нет кода ответа: %v", err)
			}
		})
	}
}

// Зависший сосед не держит воркер дольше таймаута; это ошибка транспорта,
// а не Rejected/Unavailable.
func TestRequestPosition_Timeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select { // висим, пока клиент не бросит соединение
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	start := time.Now()
	err := newClient(t, srv.URL, 50*time.Millisecond).RequestPosition(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("err = nil, want таймаут")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("err = %v, want таймаут транспорта", err)
	}
	if errors.Is(err, push.ErrRejected) || errors.Is(err, push.ErrUnavailable) {
		t.Errorf("таймаут — не ответ соседа: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("вызов шёл %v при таймауте 50ms", d)
	}
}

// Отменённый ctx (остановка сервиса) прерывает вызов: запрос собран с ctx.
func TestRequestPosition_ContextCancelled(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := newClient(t, srv.URL, time.Second).RequestPosition(ctx, uuid.New(), uuid.New())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if called.Load() {
		t.Error("запрос ушёл, хотя ctx уже отменён")
	}
}

// Тело ответа дочитывается и закрывается при любом коде: тогда соединение
// возвращается в пул и переиспользуется. Без этого — новый TCP на каждый
// вызов (а без Close ещё и утечка).
func TestRequestPosition_ReusesConnection(t *testing.T) {
	t.Parallel()

	var newConns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", 8<<10))) // тело есть, клиенту оно не нужно
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			newConns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, time.Second)
	for range 3 {
		if err := c.RequestPosition(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, push.ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	}
	if n := newConns.Load(); n != 1 {
		t.Errorf("открыто %d соединений на 3 вызова, want 1: тело ответа не дочитано или не закрыто", n)
	}
}

// Вызов Push Service — отдельный клиентский спан, дочерний к текущему,
// а контекст трейса уходит соседу в заголовке traceparent.
func TestRequestPosition_Tracing(t *testing.T) {
	t.Parallel()

	var traceparent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent.Store(r.Header.Get("traceparent"))
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	c := push.New(srv.URL, time.Second, tp, propagation.TraceContext{})
	t.Cleanup(c.Close)

	ctx, parent := tp.Tracer("test").Start(context.Background(), "worker")
	if err := c.RequestPosition(ctx, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("err = %v", err)
	}
	parent.End()

	var call sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "push.RequestPosition" {
			call = s
		}
	}
	if call == nil {
		t.Fatal("нет спана push.RequestPosition")
	}
	if call.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("спан вызова не дочерний к спану воркера: запрос собран без ctx?")
	}
	tpHeader, _ := traceparent.Load().(string)
	if !strings.Contains(tpHeader, parent.SpanContext().TraceID().String()) {
		t.Errorf("traceparent = %q, want trace id %s", tpHeader, parent.SpanContext().TraceID())
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
