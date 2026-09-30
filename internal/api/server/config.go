package server

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/DjordjeVuckovic/tusker/pkg/utils"
	"github.com/labstack/gommon/bytes"
)

const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	// DefaultWriteTimeout outlasts the slowest search: a 60s query embedding
	// followed by an Elasticsearch call bounded at 30s.
	DefaultWriteTimeout = 120 * time.Second
	DefaultIdleTimeout  = 120 * time.Second
	DefaultBodyLimit    = "1M"
)

type Config struct {
	Port        string
	UseHttp2    bool
	CorsOrigins []string

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	// BodyLimit is a size such as "1M"; larger request bodies get 413.
	BodyLimit string
}

func LoadConfig() (*Config, error) {
	err := env.LoadDotEnv("cmd/news_api/.env")
	if err != nil {
		slog.Info("Skipping .env ...", "error", err)
	}

	useHttp2Str := os.Getenv("USE_HTTP2")
	useHttp2 := useHttp2Str == "true"

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if err := validatePort(port); err != nil {
		return nil, fmt.Errorf("invalid port: %w", err)
	}

	var origins []string
	corsOriginsEnv := os.Getenv("CORS_ORIGINS")
	if corsOriginsEnv != "" {
		origins = strings.Split(corsOriginsEnv, ",")
		for i, origin := range origins {
			origins[i] = strings.TrimSpace(origin)
		}
		origins = utils.RemoveEmptyStrings(origins)
	}

	if len(origins) == 0 {
		origins = []string{"*"}
	}

	cfg := &Config{
		Port:        port,
		UseHttp2:    useHttp2,
		CorsOrigins: origins,
	}

	timeouts := map[string]*time.Duration{
		"HTTP_READ_HEADER_TIMEOUT": &cfg.ReadHeaderTimeout,
		"HTTP_READ_TIMEOUT":        &cfg.ReadTimeout,
		"HTTP_WRITE_TIMEOUT":       &cfg.WriteTimeout,
		"HTTP_IDLE_TIMEOUT":        &cfg.IdleTimeout,
	}
	for name, target := range timeouts {
		if *target, err = durationFromEnv(name); err != nil {
			return nil, err
		}
	}

	if limit := os.Getenv("HTTP_BODY_LIMIT"); limit != "" {
		if _, err := bytes.Parse(limit); err != nil {
			return nil, fmt.Errorf("invalid HTTP_BODY_LIMIT %q: %w", limit, err)
		}
		cfg.BodyLimit = limit
	}

	return cfg.withDefaults(), nil
}

// withDefaults returns a copy of cfg with every unset timeout and the body
// limit filled in, so no Config leaves the server without a bound.
func (cfg Config) withDefaults() *Config {
	defaults := []struct {
		value    *time.Duration
		fallback time.Duration
	}{
		{value: &cfg.ReadHeaderTimeout, fallback: DefaultReadHeaderTimeout},
		{value: &cfg.ReadTimeout, fallback: DefaultReadTimeout},
		{value: &cfg.WriteTimeout, fallback: DefaultWriteTimeout},
		{value: &cfg.IdleTimeout, fallback: DefaultIdleTimeout},
	}
	for _, d := range defaults {
		if *d.value == 0 {
			*d.value = d.fallback
		}
	}
	if cfg.BodyLimit == "" {
		cfg.BodyLimit = DefaultBodyLimit
	}
	return &cfg
}

func durationFromEnv(name string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid %s %q: must be positive", name, raw)
	}
	return d, nil
}

func validatePort(port string) error {
	portNum, err := strconv.Atoi(port)

	if err != nil {
		return errors.New("port must be a number")
	}

	if portNum < 1 || portNum > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	return nil
}
