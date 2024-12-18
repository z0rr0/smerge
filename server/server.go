package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/z0rr0/smerge/cfg"
	"github.com/z0rr0/smerge/crawler"
)

func requestID() string {
	var bytes = make([]byte, 16)

	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		slog.Warn("failed to generate request ID", "error", err)
		return "-"
	}
	return hex.EncodeToString(bytes)
}

func Run(config *cfg.Config) {
	var (
		serverTimeout = time.Duration(config.Timeout)
		serverAddr    = config.Addr()
		groups        = config.GroupsMap()
	)

	slog.Info("starting crawler", "groups", len(config.Groups))
	cr, cancel := crawler.New(config.Groups)
	cr.Run()

	srv := &http.Server{
		Addr:           serverAddr,
		Handler:        http.DefaultServeMux,
		ReadTimeout:    serverTimeout,
		WriteTimeout:   serverTimeout,
		MaxHeaderBytes: 1 << 10, // 1Kb
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		start, code, reqID := time.Now(), http.StatusOK, requestID()
		defer func() {
			slog.Info(
				"request", "id", reqID, "method", r.Method,
				"code", code, "duration", time.Since(start), "remote", r.RemoteAddr,
			)
		}()

		url := strings.Trim(r.URL.Path, "/ ")
		slog.Info("request", "id", reqID, "method", r.Method, "url", url, "remote", r.RemoteAddr)

		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			code = http.StatusMethodNotAllowed
			return
		}

		group, ok := groups[url]
		if !ok {
			code = http.StatusNotFound
			http.Error(w, "Not Found", code)
			return
		}

		data := cr.Get(group.Name)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(data)); err != nil {
			slog.Error("response write error", "id", reqID, "error", err)
		}
	})
	idleConnsClosed := make(chan struct{})

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, os.Signal(syscall.SIGTERM), os.Signal(syscall.SIGQUIT))
		<-sigint

		slog.Info("shutting down crawler")
		cancel()

		slog.Info("shutting down server")
		if err := srv.Shutdown(context.Background()); err != nil {
			slog.Error("HTTP server shutdown error", "error", err)
		}
		close(idleConnsClosed)
	}()

	slog.Info("starting server", "addr", serverAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server ListenAndServe error", "error", err)
	}

	cr.Stop()
	slog.Info("crawler stopped")

	<-idleConnsClosed
	slog.Info("HTTP server stopped")
}
