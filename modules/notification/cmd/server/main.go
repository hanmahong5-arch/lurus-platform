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
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/platform"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/repo"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/adapter/sender"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/auth"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/config"
	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/pkg/tracing"
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
	// --- OpenTelemetry tracing ---
	tracingShutdown, err := tracing.Init(ctx, cfg.OtelServiceName, cfg.OtelEndpoint)
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(shutCtx)
	}()

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

	// --- NATS ---
	slog.Info("connecting to nats", "addr", cfg.NATSAddr)
	nc, err := natsgo.Connect(cfg.NATSAddr,
		natsgo.Timeout(30*time.Second),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(5*time.Second),
		natsgo.DisconnectErrHandler(func(_ *natsgo.Conn, err error) {
			slog.Warn("nats disconnected", "err", err)
		}),
		natsgo.ReconnectHandler(func(_ *natsgo.Conn) {
			slog.Info("nats reconnected")
		}),
		natsgo.ConnectHandler(func(_ *natsgo.Conn) {
			slog.Info("nats connected successfully")
		}),
		natsgo.ErrorHandler(func(_ *natsgo.Conn, _ *natsgo.Subscription, err error) {
			slog.Error("nats async error", "err", err)
		}),
	)
	if err != nil {
		slog.Warn("nats connection failed, running without event consumers", "err", err)
		nc = nil
	} else {
		slog.Info("nats connect returned", "status", nc.Status().String(), "connected_addr", nc.ConnectedAddr())
	}
	if nc != nil {
		defer nc.Close()
	}

	// --- Repositories ---
	notifRepo := repo.NewNotificationRepo(db)
	tmplRepo := repo.NewTemplateRepo(db)
	prefRepo := repo.NewPreferenceRepo(db)
	deviceRepo := repo.NewDeviceTokenRepo(db)

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
	notifSvc.SetRedis(rdb)
	tmplSvc := app.NewTemplateService(tmplRepo)
	prefSvc := app.NewPreferenceService(prefRepo)
	deviceSvc := app.NewDeviceService(deviceRepo)

	// --- JWT Authentication ---
	var jwtMiddleware *auth.Middleware
	if cfg.SessionSecret != "" {
		jwtMiddleware = auth.NewMiddleware(auth.Config{
			SessionSecret:       cfg.SessionSecret,
			PlatformURL:         cfg.PlatformURL,
			PlatformInternalKey: cfg.PlatformInternalKey,
		})
		slog.Info("JWT authentication enabled")
	} else {
		slog.Warn("JWT authentication disabled (SESSION_SECRET not set), using dev X-Account-ID header")
	}

	// --- HTTP Handlers ---
	notifH := handler.NewNotificationHandler(notifSvc, wsHub, cfg.AlertAdminEmail)
	tmplH := handler.NewTemplateHandler(tmplSvc)
	prefH := handler.NewPreferenceHandler(prefSvc)
	deviceH := handler.NewDeviceHandler(deviceSvc)

	engine := handler.BuildRouter(handler.Deps{
		Notifications:   notifH,
		Templates:       tmplH,
		Preferences:     prefH,
		Devices:         deviceH,
		JWT:             jwtMiddleware,
		InternalKey:     cfg.InternalAPIKey,
		WebhookSecret:   cfg.WebhookSecret,
		OtelServiceName: cfg.OtelServiceName,
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Retry Worker ---
	notifSvc.StartRetryWorker(ctx)

	// --- Weekly Digest Worker ---
	digestFetcher := platform.NewDigestFetcher(cfg.PlatformURL, cfg.PlatformInternalKey)
	digestWorker := app.NewDigestWorker(notifSvc, digestFetcher)
	digestWorker.Start(ctx)

	// --- NATS Consumer ---
	g, gctx := errgroup.WithContext(ctx)

	// HTTP server
	g.Go(func() error {
		slog.Info("lurus-platform-notification starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	if nc != nil {
		consumer, err := natsconsumer.NewConsumer(nc, notifSvc, rdb)
		if err != nil {
			slog.Warn("nats consumer init failed", "err", err)
		} else {
			g.Go(func() error {
				return consumer.Run(gctx)
			})
		}
	} else {
		slog.Warn("nats consumer disabled (no connection)")
	}

	// Graceful shutdown
	g.Go(func() error {
		<-gctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutCtx)
	})

	return g.Wait()
}
