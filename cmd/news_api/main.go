// Package main Tusker API
// @title Tusker API
// @version 1.0
// @description A full-text and semantic search engine for exploring multilingual news headlines and articles
// @termsOfService http://swagger.io/terms/
// @contact.name API Support
// @contact.email support@tusker.sh
// @license.name Apache 2.0
// @license.url https://opensource.org/licenses/Apache-2.0
// @BasePath /
package main

import (
	"log/slog"
	"os"

	"github.com/DjordjeVuckovic/tusker/internal/api/router"
	server2 "github.com/DjordjeVuckovic/tusker/internal/api/server"
	"github.com/DjordjeVuckovic/tusker/internal/embedding"
	"github.com/DjordjeVuckovic/tusker/internal/storage/factory"
	pkgserver "github.com/DjordjeVuckovic/tusker/pkg/server"
	"github.com/labstack/echo/v4"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	sCfg, err := server2.LoadConfig()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	heathChecker := pkgserver.NewOkHealthChecker()

	s := server2.New(sCfg, heathChecker).
		SetupMiddlewares().
		SetupValidator().
		SetupErrorHandler().
		SetupHealthChecks("/health").
		SetupOpenApi("/swagger/*")

	s.Echo.GET("/", func(c echo.Context) error {
		return c.String(200, "Tusker API is running")
	})

	appSettings := NewAppConfig()
	cfg, err := appSettings.Load()
	if err != nil {
		slog.Error("Failed to load app configuration", "error", err)
		os.Exit(1)
		return
	}

	searcher, err := factory.NewSearcher(s.Context(), cfg.StorageConfig)
	if err != nil {
		slog.Error("Failed to create storage searcher", "error", err)
		os.Exit(1)
		return
	}

	var routerOpts []router.SearchRouterOption
	if cfg.EmbeddingConfig.Enabled {
		embedClient, err := embedding.NewOllamaClient(cfg.EmbeddingConfig.BaseURL)
		if err != nil {
			slog.Error("Failed to create embedding client", "error", err)
			os.Exit(1)
			return
		}
		semanticSearcher, err := factory.NewSemanticSearcher(s.Context(), cfg.StorageConfig, embedClient)
		if err != nil {
			slog.Error("Failed to create semantic searcher", "error", err)
			os.Exit(1)
			return
		}
		routerOpts = append(routerOpts, router.WithSemanticSearcher(semanticSearcher))
		slog.Info("Semantic search enabled")

		hybridSearcher, err := factory.NewHybridSearcher(s.Context(), cfg.StorageConfig, embedClient)
		if err != nil {
			slog.Warn("Hybrid search disabled: failed to create hybrid searcher", "error", err)
		} else {
			routerOpts = append(routerOpts, router.WithHybridSearcher(hybridSearcher))
			slog.Info("Hybrid search enabled")
		}
	} else {
		slog.Info("Semantic search disabled")
	}

	searchrouter := router.NewSearchRouter(s.Echo, searcher, routerOpts...)
	searchrouter.Bind()

	go func() {
		<-s.ShutdownSignal()
		slog.Info("Shutdown started, cleaning up resources...")
	}()

	err = s.Start()
	if err != nil {
		s.Echo.Logger.Error("Failed to start server: ", err)
		os.Exit(1)
	}
}
