package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/donychen1134/pupbox/internal/dashscopeapi"
	"github.com/donychen1134/pupbox/internal/openaiapi"
	"github.com/donychen1134/pupbox/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	addr := envDefault("PUPBOX_ADDR", "127.0.0.1:8787")
	openAI := openaiapi.NewFromEnv()

	srv := server.New(server.Config{
		AI:        openAI,
		Voice:     selectVoiceProvider(openAI),
		StaticDir: "web/static",
		Logger:    logger,
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("pupbox server started", "url", "http://"+addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("pupbox server stopped")
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func selectVoiceProvider(openAI *openaiapi.Client) server.VoiceProvider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("PUPBOX_VOICE_PROVIDER")))
	switch provider {
	case "mock", "none", "off":
		return nil
	case "dashscope", "aliyun", "qwen":
		return dashscopeapi.NewFromEnv()
	case "openai":
		return openAI
	}

	dashscope := dashscopeapi.NewFromEnv()
	if dashscope.Available() {
		return dashscope
	}
	return openAI
}
