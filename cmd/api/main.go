// Command api runs the HTTP API server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"shopee-automation/internal/app"
	"shopee-automation/internal/identity"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/config"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/logx"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/otelx"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/shopee"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	log := logx.New(os.Stdout, cfg.LogLevel).With(slog.String("service", "api"))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := otelx.Setup(ctx, cfg.OTLPEndpoint, "shopee-automation-api", cfg.Env)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()

	ropt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(ropt)
	defer func() { _ = rdb.Close() }()

	var mail mailer.Mailer = mailer.Log{Logger: log}
	if cfg.SMTPAddr != "" {
		mail = mailer.SMTP{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom}
	}

	keys, err := envelope.ParseKeyring(cfg.EncryptionKeys, cfg.EncryptionActiveKey)
	if err != nil {
		return err
	}
	limiter := ratelimit.Redis{Client: rdb, Prefix: "rl:"}
	tasks := asynq.NewClientFromRedisClient(rdb)
	defer func() { _ = tasks.Close() }()

	a := app.New(app.Deps{
		Config: cfg, DB: database, Redis: rdb,
		Limiter: limiter,
		Mailer:  mail, Clock: clock.Real{}, Log: log,
		Hasher: identity.Hasher{Params: identity.DefaultArgon2},
		Keys:   keys,
		Shopee: shopee.NewClient(cfg, limiter),
		Queue:  queue.Asynq{Client: tasks},
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", cfg.HTTPAddr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			return err
		}
	}
	return nil
}
