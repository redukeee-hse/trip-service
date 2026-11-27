// Package push — гейтвей в Push Service: единственное место, которое знает
// его HTTP-контракт (contracts/openapi/push-service.openapi.yaml).
//
// Остальной сервис зовёт RequestPosition через свой узкий интерфейс
// (объявлен у потребителя — в worker/positions) и не знает ни URL, ни JSON,
// ни кодов ответа. Перейдём в ЛР4 на gRPC — поменяется только этот пакет.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	sendPushPath        = "/api/v1/push"
	kindRequestPosition = "REQUEST_POSITION"
)

// pushRequest — тело POST /api/v1/push (схема PushRequest в контракте).
type pushRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data,omitempty"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

// New создаёт клиент со своим http.Client, а не http.DefaultClient:
//   - у DefaultClient нет таймаута вообще — зависший сосед навсегда займёт
//     воркер;
//   - DefaultClient общий на весь процесс: любая библиотека может поменять
//     его настройки, и это отразится на нас.
//
// timeout — потолок на весь вызов: соединение, отправка, ожидание и чтение
// тела ответа. Дедлайн из ctx тоже действует — сработает тот, что раньше.
//
// Транспорт обёрнут otelhttp: на каждый вызов — клиентский спан (это и есть
// «вызов Push Service — отдельный спан» из ТЗ), а в заголовок traceparent
// кладётся контекст трейса, чтобы сосед мог продолжить наш трейс.
func New(baseURL string, timeout time.Duration, tp trace.TracerProvider, prop propagation.TextMapPropagator) *Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	transport := otelhttp.NewTransport(base.Clone(), // Clone: свой пул соединений, общий DefaultTransport не трогаем
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithPropagators(prop),
		// Длительность вызова считаем своей метрикой с лейблом result
		// (tripgo.position_requests.duration), стандартные метрики otelhttp
		// здесь не нужны.
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(func(_ string, _ *http.Request) string {
			return "push.RequestPosition"
		}),
	)
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout, Transport: transport},
	}
}

// RequestPosition просит устройство водителя прислать координату по поездке.
// Одна попытка, без ретраев (они — ЛР5).
//
// Ошибки: ErrRejected (4xx), ErrUnavailable (5xx, 429), иначе — ошибка
// транспорта (таймаут, нет соединения).
func (c *Client) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	body, err := json.Marshal(pushRequest{
		// request_id — для корреляции логов у соседа; свой на каждый вызов.
		RequestID:   uuid.NewString(),
		RecipientID: driverID,
		Kind:        kindRequestPosition,
		Data:        map[string]string{"trip_id": tripID.String()},
	})
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+sendPushPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Сюда попадают таймаут, отказ в соединении, отмена ctx — ответа нет.
		return fmt.Errorf("send push: %w", err)
	}
	// Тело закрываем всегда, при любом коде. Сначала дочитываем остаток:
	// иначе соединение не вернётся в пул и следующий вызов откроет новое.
	// Ошибки здесь игнорируем осознанно — ответ уже разобран, а сломанное
	// соединение транспорт просто выбросит.
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch code := resp.StatusCode; {
	case code == http.StatusAccepted:
		return nil
	// 429 проверяем раньше общего 4xx: это лимит частоты — временно, как 5xx.
	case code == http.StatusTooManyRequests, code >= 500:
		return fmt.Errorf("push: status %d: %w", code, ErrUnavailable)
	case code >= 400:
		return fmt.Errorf("push: status %d: %w", code, ErrRejected)
	default:
		// 1xx/2xx/3xx кроме 202 — контракт обещает только 202.
		return fmt.Errorf("push: unexpected status %d", code)
	}
}

// Close закрывает простаивающие соединения пула. Вызывается при остановке
// сервиса после воркеров (ЛР3 п. 5.7, шаг 4).
func (c *Client) Close() {
	c.http.CloseIdleConnections()
}
