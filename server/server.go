package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/crawler"
)

func Run(config *cfg.Config, versionInfo string) {
	var (
		serverTimeout   = time.Duration(config.Timeout)
		serverAddr      = config.Addr()
		groupsEndpoints = config.GroupsEndpoints()
	)

	slog.Info("starting crawler", "groups", len(config.Groups))
	cr := crawler.New(config.Groups, config.UserAgent, config.Retries, int(config.MaxConcurrent))
	cr.Run()

	handler := LoggingMiddleware(
		ErrorHandlingMiddleware(
			ValidationMiddleware(
				HealthCheckMiddleware(
					handleGroup(groupsEndpoints, cr),
					versionInfo,
				),
			),
		),
	)

	srv := &http.Server{
		Addr:           serverAddr,
		Handler:        handler,
		ReadTimeout:    serverTimeout,
		WriteTimeout:   serverTimeout,
		MaxHeaderBytes: 1 << 16, // 64Kb
	}
	serverStopped := make(chan struct{})

	sigint := make(chan os.Signal, 1)
	go func() {
		signal.Notify(sigint, os.Interrupt, os.Signal(syscall.SIGTERM), os.Signal(syscall.SIGQUIT))
		<-sigint

		slog.Info("shutting down crawler")
		cr.Shutdown()
		slog.Info("crawler stopped")

		slog.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
		}
		close(serverStopped)
	}()

	slog.Info("starting server", "addr", serverAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server ListenAndServe error", "error", err)
		sigint <- os.Interrupt
	}

	<-serverStopped
	slog.Info("HTTP server stopped")
}
