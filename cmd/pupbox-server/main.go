package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/donychen1134/pupbox/internal/dashscopeapi"
	"github.com/donychen1134/pupbox/internal/dog"
	"github.com/donychen1134/pupbox/internal/openaiapi"
	"github.com/donychen1134/pupbox/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	addr := envDefault("PUPBOX_ADDR", "127.0.0.1:8787")
	openAI := openaiapi.NewFromEnv()
	dashscope := dashscopeapi.NewFromEnv()

	srv := server.New(server.Config{
		Chat:             selectChatProvider(openAI, dashscope),
		Voice:            selectVoiceProvider(openAI, dashscope),
		StaticDir:        "web/static",
		AccessToken:      os.Getenv("PUPBOX_ACCESS_TOKEN"),
		EventLogPath:     envDefault("PUPBOX_EVENT_LOG_PATH", "data/events.jsonl"),
		EventLogLimit:    envInt("PUPBOX_EVENT_LOG_LIMIT", 500),
		RecordingDir:     os.Getenv("PUPBOX_RECORDING_DIR"),
		RecordingLimit:   envInt("PUPBOX_RECORDING_LIMIT", 20),
		TrimSTTSilence:   envBool("PUPBOX_STT_TRIM_SILENCE", true),
		SpeechCacheDir:   envDefault("PUPBOX_TTS_CACHE_DIR", "data/tts-cache"),
		SpeechCacheLimit: envInt("PUPBOX_TTS_CACHE_LIMIT", 512),
		Logger:           logger,
	})
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

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
	if envBool("PUPBOX_TTS_PREWARM", true) {
		go func() {
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
				replies := dog.PrewarmReplies()
				if limit := envInt("PUPBOX_TTS_PREWARM_LIMIT", 80); limit > 0 && limit < len(replies) {
					replies = replies[:limit]
				}
				srv.PrewarmSpeech(runCtx, replies)
			case <-runCtx.Done():
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	cancelRun()

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

func envInt(key string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func selectChatProvider(openAI *openaiapi.Client, dashscope *dashscopeapi.Client) server.ChatProvider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("PUPBOX_CHAT_PROVIDER")))
	switch provider {
	case "mock", "local", "none", "off":
		return nil
	case "dashscope", "aliyun", "qwen":
		return dashscope
	case "openai":
		return openAI
	}

	if dashscope.Available() {
		return dashscope
	}
	return openAI
}

func selectVoiceProvider(openAI *openaiapi.Client, dashscope *dashscopeapi.Client) server.VoiceProvider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("PUPBOX_VOICE_PROVIDER")))
	switch provider {
	case "mock", "none", "off":
		return nil
	case "dashscope", "aliyun", "qwen":
		return dashscope
	case "openai":
		return openAI
	}

	if dashscope.Available() {
		return dashscope
	}
	return openAI
}
