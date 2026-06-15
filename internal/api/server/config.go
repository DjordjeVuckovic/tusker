package server

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/DjordjeVuckovic/tusker/pkg/config/env"
	"github.com/DjordjeVuckovic/tusker/pkg/utils"
)

type Config struct {
	Port        string
	UseHttp2    bool
	CorsOrigins []string
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

	return &Config{
		Port:        port,
		UseHttp2:    useHttp2,
		CorsOrigins: origins,
	}, nil
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
