package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jittair/waypoint/backend/internal/api"
	"github.com/jittair/waypoint/backend/internal/auth"
	"github.com/jittair/waypoint/backend/internal/calendar"
	"github.com/jittair/waypoint/backend/internal/importer"
	"github.com/jittair/waypoint/backend/internal/integrations"
	"github.com/jittair/waypoint/backend/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := mustEnv("DATABASE_URL")
	supabaseURL := mustEnv("SUPABASE_URL")
	ctx := context.Background()

	st, err := store.New(ctx, databaseURL)
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	verifier, err := auth.NewVerifier(ctx, supabaseURL)
	if err != nil {
		logger.Error("configure token verifier", "error", err)
		os.Exit(1)
	}
	var extractor importer.Extractor
	if apiKey, model := env("OPENROUTER_API_KEY", ""), env("OPENROUTER_MODEL", ""); apiKey != "" && model != "" {
		timeout := time.Duration(envInt("OPENROUTER_TIMEOUT_SECONDS", 30)) * time.Second
		extractor, err = importer.NewOpenRouterExtractor(apiKey, model, timeout)
		if err != nil {
			logger.Error("configure OpenRouter extractor", "error", err)
			os.Exit(1)
		}
	}
	importProcessor := &importer.Processor{Logger: logger, Store: st, Resend: integrations.NewResendClient(env("RESEND_API_KEY", "")), Storage: integrations.NewStorage(supabaseURL, env("SUPABASE_SERVICE_ROLE_KEY", "")), Extractor: extractor}
	calendarService := calendar.New(calendar.Config{ClientID: env("GOOGLE_CALENDAR_CLIENT_ID", ""), ClientSecret: env("GOOGLE_CALENDAR_CLIENT_SECRET", ""), RedirectURL: env("GOOGLE_CALENDAR_REDIRECT_URL", ""), StateSecret: env("GOOGLE_OAUTH_STATE_SECRET", ""), TokenKey: env("GOOGLE_TOKEN_ENCRYPTION_KEY", "")}, st)

	server := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           api.New(st, verifier, logger, api.Config{AllowedOrigins: strings.Split(env("CORS_ORIGINS", "http://localhost:5173"), ","), InboundDomain: env("RESEND_INBOUND_DOMAIN", ""), InboundAddress: env("RESEND_INBOUND_ADDRESS", ""), InboundOwnerID: env("RESEND_INBOUND_OWNER_ID", ""), ResendWebhookSecret: env("RESEND_WEBHOOK_SECRET", ""), ImportProcessor: importProcessor, Calendar: calendarService, AppURL: env("APP_URL", "http://localhost:5173"), BasePath: env("API_BASE_PATH", ""), CronSecret: env("CRON_SECRET", ""), CronBudget: time.Duration(envInt("CRON_BUDGET_SECONDS", 50)) * time.Second, InboxBudget: time.Duration(envInt("INBOX_BUDGET_SECONDS", 20)) * time.Second}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	// A background ticker only advances while the process is scheduled. Hosts that
	// suspend between requests must drive the worker from a scheduler instead, via
	// the /internal/cron/worker endpoint.
	if strings.EqualFold(env("WORKER_MODE", "loop"), "cron") {
		logger.Info("background worker disabled; expecting scheduled calls to /internal/cron/worker")
	} else {
		go runWorkers(ctx, logger, importProcessor, calendarService)
	}

	go func() {
		logger.Info("API listening", "address", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()
	signalContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-signalContext.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownContext)
}

func runWorkers(ctx context.Context, logger *slog.Logger, imports *importer.Processor, calendars *calendar.Service) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := imports.RunOnce(ctx); err != nil {
			logger.Error("reservation import worker", "error", err)
		}
		if calendars != nil {
			if err := calendars.RunOnce(ctx); err != nil {
				logger.Error("calendar sync worker", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func mustEnv(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	slog.Error("missing required environment variable", "name", key)
	os.Exit(1)
	return ""
}
