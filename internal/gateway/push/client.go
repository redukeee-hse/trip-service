// Package push — гейтвей в Push Service по HTTP
// (contracts/openapi/push-service.openapi.yaml).
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

// New создаёт клиент со своим http.Client: у http.DefaultClient нет таймаута.
// otelhttp даёт спан на каждый вызов и передаёт соседу traceparent.
func New(baseURL string, timeout time.Duration, tp trace.TracerProvider, prop propagation.TextMapPropagator) *Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	transport := otelhttp.NewTransport(base.Clone(),
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithPropagators(prop),
		// Длительность пишем своей метрикой tripgo.position_requests.duration.
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

// RequestPosition просит устройство водителя прислать координату. Одна
// попытка. Ошибки: ErrRejected, ErrUnavailable или ошибка транспорта.
func (c *Client) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	body, err := json.Marshal(pushRequest{
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
		return fmt.Errorf("send push: %w", err)
	}
	// Дочитываем тело до конца, иначе соединение не вернётся в пул.
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	switch code := resp.StatusCode; {
	case code == http.StatusAccepted:
		return nil
	case code == http.StatusTooManyRequests, code >= 500:
		return fmt.Errorf("push: status %d: %w", code, ErrUnavailable)
	case code >= 400:
		return fmt.Errorf("push: status %d: %w", code, ErrRejected)
	default:
		return fmt.Errorf("push: unexpected status %d", code)
	}
}

// Close закрывает простаивающие соединения. Вызывается после остановки воркеров.
func (c *Client) Close() {
	c.http.CloseIdleConnections()
}
