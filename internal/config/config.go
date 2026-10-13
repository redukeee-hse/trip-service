package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr              string
	LogLvl            slog.Level
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	PingTimeout       time.Duration
	DB                DBConfig
}

type DBConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
	QueryTimeout    time.Duration
}

func LoadConfig() (Config, error) {
	addr, err := getStringEnv(
		"HTTP_ADDR",
		":8080",
	)
	if err != nil {
		return Config{}, err
	}

	logLvl, err := getLogLevelEnv(
		"LOG_LEVEL",
		slog.LevelInfo,
	)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := getDurationEnv(
		"SHUTDOWN_TIMEOUT",
		10*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return Config{}, errors.New("DATABASE_URL не задан")
	}

	maxConns, err := getInt32Env("DATABASE_MAX_CONNS", 10)
	if err != nil {
		return Config{}, err
	}

	minConns, err := getInt32EnvMin("DATABASE_MIN_CONNS", 2)
	if err != nil {
		return Config{}, err
	}

	maxConnLifetime, err := getDurationEnv(
		"DATABASE_MAX_CONN_LIFETIME",
		30*time.Minute,
	)
	if err != nil {
		return Config{}, err
	}

	connectTimeout, err := getDurationEnv(
		"DATABASE_CONNECT_TIMEOUT",
		5*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	queryTimeout, err := getDurationEnv(
		"DATABASE_QUERY_TIMEOUT",
		3*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	if minConns > maxConns {
		return Config{}, fmt.Errorf(
			"DATABASE_MIN_CONNS не может быть больше DATABASE_MAX_CONNS",
		)
	}

	readHeaderTimeout, err := getDurationEnv(
		"HTTP_READ_HEADER_TIMEOUT",
		5*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	readTimeout, err := getDurationEnv(
		"HTTP_READ_TIMEOUT",
		10*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	writeTimeout, err := getDurationEnv(
		"HTTP_WRITE_TIMEOUT",
		15*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	idleTimeout, err := getDurationEnv(
		"HTTP_IDLE_TIMEOUT",
		60*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	pingTimeout, err := getDurationEnv(
		"HTTP_PING_TIMEOUT",
		time.Second,
	)

	return Config{
		Addr:              addr,
		LogLvl:            logLvl,
		ShutdownTimeout:   shutdownTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		PingTimeout:       pingTimeout,
		DB: DBConfig{
			URL:             url,
			MaxConns:        maxConns,
			MinConns:        minConns,
			MaxConnLifetime: maxConnLifetime,
			ConnectTimeout:  connectTimeout,
			QueryTimeout:    queryTimeout,
		},
	}, nil
}

func getStringEnv(name string, defaultValue string) (string, error) {
	value := os.Getenv(name)

	if value == "" {
		return defaultValue, nil
	}

	return value, nil
}

func getInt32Env(name string, defaultValue int32) (int32, error) {
	value := os.Getenv(name)

	if value == "" {
		return defaultValue, nil
	}

	result, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf(
			"%s должно быть целым числом: %w",
			name,
			err,
		)
	}

	if result <= 0 {
		return 0, fmt.Errorf("%s должно быть больше нуля", name)
	}

	return int32(result), nil
}

func getInt32EnvMin(name string, defaultValue int32) (int32, error) {
	value := os.Getenv(name)

	if value == "" {
		return defaultValue, nil
	}

	result, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf(
			"%s должно быть целым числом: %w",
			name,
			err,
		)
	}

	if result < 0 {
		return 0, fmt.Errorf("%s должно быть больше нуля", name)
	}

	return int32(result), nil
}

func getDurationEnv(name string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(name)

	if value == "" {
		return defaultValue, nil
	}

	result, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf(
			"%s должно иметь формат вроде 5s, 10m или 1h: %w",
			name,
			err,
		)
	}

	if result <= 0 {
		return 0, fmt.Errorf("%s должно быть больше нуля", name)
	}

	return result, nil
}

func getLogLevelEnv(name string, defaultValue slog.Level) (slog.Level, error) {
	value := os.Getenv(name)

	if value == "" {
		return defaultValue, nil
	}

	var lvl slog.Level
	err := lvl.UnmarshalText([]byte(value))
	if err != nil {
		return 0, fmt.Errorf(
			"%s должен быть debug, info, warn или error: %w",
			name,
			err,
		)
	}

	return lvl, nil
}
