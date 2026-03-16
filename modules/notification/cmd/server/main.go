package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	natsgo "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/handler"
	natsconsumer "github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/nats"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/sender"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/config"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	if cfg.Env == "production" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	// --- Database ---
	db, err := gorm.Open(postgres.Open(cfg.DatabaseDSN), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(3)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	defer sqlDB.Close()

	// --- Redis ---
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer rdb.Close()
	_ = rdb // reserved for future caching/dedup

	// --- NATS ---
	nc, err := natsgo.Connect(cfg.NATSAddr,
		natsgo.RetryOnFailedConnect(true),
		natsgo.MaxReconnects(10),
		natsgo.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	// --- Repositories ---
	notifRepo := repo.NewNotificationRepo(db)
	tmplRepo := repo.NewTemplateRepo(db)
	prefRepo := repo.NewPreferenceRepo(db)

	// --- Senders ---
	var emailSender sender.Sender
	if s := sender.NewEmailSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.EmailFrom); s != nil {
		emailSender = s
		slog.Info("email sender enabled", "host", cfg.SMTPHost)
	} else {
		emailSender = sender.NoopEmailSender{}
		slog.Warn("email sender disabled (SMTP_HOST not set)")
	}
	fcmSender := sender.NewFCMSender(cfg.FCMCredentials)

	wsHub := sender.NewHub()
	wsSender := sender.NewWSSender(wsHub)

	// --- App Services ---
	notifSvc := app.NewNotificationService(notifRepo, tmplRepo, prefRepo, emailSender, fcmSender, wsSender, wsHub)
	tmplSvc := app.NewTemplateService(tmplRepo)

	// --- HTTP Handlers ---
	notifH := handler.NewNotificationHandler(notifSvc, wsHub, cfg.AlertAdminEmail)
	tmplH := handler.NewTemplateHandler(tmplSvc)

	engine := handler.BuildRouter(handler.Deps{
		Notifications: notifH,
		Templates:     tmplH,
		InternalKey:   cfg.InternalAPIKey,
		WebhookSecret: cfg.WebhookSecret,
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- NATS Consumer ---
	consumer, err := natsconsumer.NewConsumer(nc, notifSvc)
	if err != nil {
		return fmt.Errorf("nats consumer: %w", err)
	}

	g, gctx := errgroup.WithContext(ctx)

	// HTTP server
	g.Go(func() error {
		slog.Info("lurus-platform-notification starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	// NATS consumer
	g.Go(func() error {
		return consumer.Run(gctx)
	})

	// Graceful shutdown
	g.Go(func() error {
		<-gctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutCtx)
	})

	return g.Wait()
}
