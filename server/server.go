package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/crawler"
	"github.com/z0rr0/smerge/limiter"
)

func runLimiter(ctx context.Context, config *cfg.Config) (*limiter.IPRateLimiter, chan struct{}) {
	const noRate = 0.0

	if config.Limiter.Rate == noRate || config.Limiter.Burst == noRate {
		slog.Info("IP rate limiting disabled")
		done := make(chan struct{})
		close(done)
		return nil, done
	}

	interval := config.Limiter.Interval.Timed()
	excluded := config.Limiter.ExcludedIPS()

	ipLimiter := limiter.NewIPRateLimiter(config.Limiter.Rate, config.Limiter.Burst, interval, excluded)
	interval = config.Limiter.CleanInterval.Timed()

	return ipLimiter, ipLimiter.Cleanup(ctx, interval, interval)
}

func Run(config *cfg.Config, versionInfo string, signals ...os.Signal) {
	var (
		serverTimeout   = time.Duration(config.Timeout)
		serverAddr      = config.Addr()
		groupsEndpoints = config.GroupsEndpoints()
	)

	limiterCtx, limiterCancel := context.WithCancel(context.Background())
	ipLimiter, limiterDone := runLimiter(limiterCtx, config)
	activeLimiter := ipLimiter != nil

	slog.Info("starting crawler", "groups", len(config.Groups))
	cr := crawler.New(config.Groups, config.UserAgent, config.Retries, int(config.Limiter.MaxConcurrent), config.Root)
	cr.Run()

	handler := LoggingMiddleware(
		ErrorHandlingMiddleware(
			RateLimiterMiddleware(
				ValidationMiddleware(
					HealthCheckMiddleware(
						handleGroup(groupsEndpoints, cr),
						versionInfo,
					),
				),
				ipLimiter,
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
		signal.Notify(sigint, signals...)
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

	limiterCancel()
	<-limiterDone
	slog.Info("IP rate limiter shutdown complete", "activated", activeLimiter)
}
