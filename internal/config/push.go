package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"
)

// PushConfig — куда и с каким таймаутом ходить в Push Service (ЛР3 п. 5.5).
type PushConfig struct {
	URL            string        // PUSH_HTTP_URL, базовый адрес без пути, например http://localhost:23809
	RequestTimeout time.Duration // PUSH_REQUEST_TIMEOUT: потолок на весь вызов, от соединения до тела ответа
}

func loadPush() (PushConfig, error) {
	raw := os.Getenv("PUSH_HTTP_URL")
	if raw == "" {
		return PushConfig{}, errors.New("PUSH_HTTP_URL не задан")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return PushConfig{}, fmt.Errorf("PUSH_HTTP_URL должен быть вида http://host:port, получено %q", raw)
	}

	timeout, err := getDurationEnv("PUSH_REQUEST_TIMEOUT", time.Second)
	if err != nil {
		return PushConfig{}, err
	}
	return PushConfig{URL: raw, RequestTimeout: timeout}, nil
}
