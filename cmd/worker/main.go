// Command worker runs background tasks (shop sync, token refresh, webhook
// processing). With -scheduler it also enqueues the periodic tasks; run
// exactly one scheduler per deployment.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
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
	"shopee-automation/internal/platform/maintenance"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/platform/otelx"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/platform/storage"
	"shopee-automation/internal/shopee"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	withScheduler := flag.Bool("scheduler", false, "also run the periodic task scheduler")
	concurrency := flag.Int("concurrency", 10, "number of concurrent tasks")
	flag.Parse()

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.DatabaseSystemURL == "" {
		return errors.New("config DATABASE_SYSTEM_URL: is required for the worker")
	}
	log := logx.New(os.Stdout, cfg.LogLevel).With(slog.String("service", "worker"))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := otelx.Setup(ctx, cfg.OTLPEndpoint, "shopee-automation-worker", cfg.Env)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	system, err := db.Open(ctx, cfg.DatabaseSystemURL)
	if err != nil {
		return err
	}
	defer system.Close()

	ropt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(ropt)
	defer func() { _ = rdb.Close() }()

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

	var signer *browser.Signer
	if cfg.RecipeSigningKey != "" {
		if signer, err = browser.NewSigner(cfg.RecipeSigningKeyID, cfg.RecipeSigningKey); err != nil {
			return err
		}
	}
	a := app.New(app.Deps{
		Config: cfg, DB: database, SystemDB: system, Redis: rdb, Limiter: limiter, Signer: signer,
		Mailer: mailer.Log{Logger: log}, Clock: clock.Real{}, Log: log,
		Hasher: identity.Hasher{Params: identity.DefaultArgon2},
		Keys:   keys, Shopee: shopee.NewClient(cfg, limiter), Queue: queue.Asynq{Client: tasks},
		Store: store, Fetcher: media.NewFetcher(30*time.Second, false),
	})
	svc := a.Shops

	mux := &queue.Mux{Log: log}
	mux.Handle(queue.TypeShopSync, svc.HandleSync)
	mux.Handle(queue.TypeTokenRefresh, svc.HandleRefresh)
	mux.Handle(queue.TypeWebhookProcess, svc.HandleWebhook)
	mux.Handle(queue.TypeTokenRefreshDue, func(ctx context.Context, _ []byte) error {
		n, err := svc.EnqueueDueRefreshes(ctx)
		log.InfoContext(ctx, "due refreshes enqueued", slog.Int("count", n))
		return err
	})
	mux.Handle(queue.TypeWebhookSweep, func(ctx context.Context, _ []byte) error {
		_, err := svc.SweepWebhooks(ctx)
		return err
	})
	mux.Handle(queue.TypeStepExecute, a.Engine.HandleExecute)
	mux.Handle(queue.TypeImport, a.Products.HandleImport)
	mux.Handle(queue.TypeStepSweep, func(ctx context.Context, _ []byte) error {
		n, err := a.Engine.Sweep(ctx)
		if n > 0 {
			log.InfoContext(ctx, "workflow steps re-enqueued", slog.Int("count", n))
		}
		return err
	})
	mux.Handle(queue.TypePartitions, func(ctx context.Context, _ []byte) error {
		return maintenance.EnsurePartitions(ctx, system, time.Now(), 3)
	})

	redisConn := asynq.RedisClientOpt{Addr: ropt.Addr, Username: ropt.Username, Password: ropt.Password, DB: ropt.DB, TLSConfig: ropt.TLSConfig}
	srv := asynq.NewServer(redisConn, asynq.Config{
		Concurrency: *concurrency,
		Logger:      asynqLogger{log},
		BaseContext: func() context.Context { return context.WithoutCancel(ctx) },
	})

	var sched *asynq.Scheduler
	if *withScheduler {
		sched = asynq.NewScheduler(redisConn, &asynq.SchedulerOpts{Logger: asynqLogger{log}})
		// Token refresh: credentials expiring within 30 minutes (shopee-integration.md §4.3).
		if _, err := sched.Register("*/5 * * * *", asynq.NewTask(queue.TypeTokenRefreshDue, nil), asynq.Unique(4*time.Minute)); err != nil {
			return err
		}
		if _, err := sched.Register("*/2 * * * *", asynq.NewTask(queue.TypeWebhookSweep, nil), asynq.Unique(90*time.Second)); err != nil {
			return err
		}
		// Workflow steps without a live task (workflow-design.md §4).
		if _, err := sched.Register("@every 15s", asynq.NewTask(queue.TypeStepSweep, nil), asynq.Unique(14*time.Second)); err != nil {
			return err
		}
		// Monthly partitions for audit_logs and workflow_events, 3 months ahead.
		if _, err := sched.Register("17 3 * * *", asynq.NewTask(queue.TypePartitions, nil), asynq.Unique(time.Hour)); err != nil {
			return err
		}
		if err := sched.Start(); err != nil {
			return err
		}
		defer sched.Shutdown()
	}

	if err := srv.Start(mux.ServeMux()); err != nil {
		return err
	}
	log.Info("worker started", slog.Bool("scheduler", *withScheduler))
	<-ctx.Done()
	log.Info("shutting down")
	srv.Shutdown()
	return nil
}

// asynqLogger routes asynq logs into slog.
type asynqLogger struct{ l *slog.Logger }

func (a asynqLogger) Debug(args ...any) { a.l.Debug("asynq", slog.Any("args", args)) }
func (a asynqLogger) Info(args ...any)  { a.l.Info("asynq", slog.Any("args", args)) }
func (a asynqLogger) Warn(args ...any)  { a.l.Warn("asynq", slog.Any("args", args)) }
func (a asynqLogger) Error(args ...any) { a.l.Error("asynq", slog.Any("args", args)) }
func (a asynqLogger) Fatal(args ...any) { a.l.Error("asynq fatal", slog.Any("args", args)); os.Exit(1) }
