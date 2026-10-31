// Command api runs the HTTP API server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"shopee-automation/internal/app"
	"shopee-automation/internal/browser"
	"shopee-automation/internal/identity"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/config"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/logx"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/platform/otelx"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/platform/storage"
	"shopee-automation/internal/shopee"
)

func main() {
	// `api -healthcheck` lets the container runtime probe the server: the
	// distroless image has no curl.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("api exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil) // #nosec G704 -- see below
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	res, err := http.DefaultClient.Do(req) // #nosec G704 -- the operator-set HTTP_ADDR of this same process
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		fmt.Fprintln(os.Stderr, "healthcheck: status", res.StatusCode)
		return 1
	}
	return 0
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
		mail = mailer.SMTP{Addr: cfg.SMTPAddr, From: cfg.SMTPFrom, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword}
	}

	keys, err := envelope.ParseKeyring(cfg.EncryptionKeys, cfg.EncryptionActiveKey)
	if err != nil {
		return err
	}
	limiter := ratelimit.Redis{Client: rdb, Prefix: "rl:"}
	store, err := storage.NewS3(ctx, storage.Config{Endpoint: cfg.S3Endpoint, AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey,
		Bucket: cfg.S3Bucket, Region: cfg.S3Region, UseSSL: cfg.S3UseSSL})
	if err != nil {
		return err
	}
	tasks := asynq.NewClientFromRedisClient(rdb)
	defer func() { _ = tasks.Close() }()

	// The system pool finds the organization of an extension token.
	var system *db.DB
	if cfg.DatabaseSystemURL != "" {
		if system, err = db.Open(ctx, cfg.DatabaseSystemURL); err != nil {
			return err
		}
		defer system.Close()
	} else {
		log.Warn("DATABASE_SYSTEM_URL is not set: the browser extension API is unavailable")
	}
	var signer *browser.Signer
	if cfg.RecipeSigningKey != "" {
		if signer, err = browser.NewSigner(cfg.RecipeSigningKeyID, cfg.RecipeSigningKey); err != nil {
			return err
		}
	}
	a := app.New(app.Deps{
		Config: cfg, DB: database, SystemDB: system, Redis: rdb, Signer: signer,
		Limiter: limiter,
		Mailer:  mail, Clock: clock.Real{}, Log: log,
		Hasher: identity.Hasher{Params: identity.DefaultArgon2},
		Keys:   keys,
		Shopee: shopee.NewClient(cfg, limiter),
		Queue:  queue.Asynq{Client: tasks},
		Store:  store, Fetcher: media.NewFetcher(30*time.Second, false),
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
