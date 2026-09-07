package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	kafka "github.com/segmentio/kafka-go"

	"github.com/milosursulovic/nebula/internal/audit"
	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/common"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/job"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/outbox"
	"github.com/milosursulovic/nebula/pkg/api"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("nebula-api exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := common.Load()
	if err != nil {
		return err
	}

	pool, err := common.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tokens := auth.NewTokenIssuer(cfg.JWTSecret)
	authRepo := auth.NewRepository(pool)
	authSvc := auth.NewService(authRepo, tokens)

	nodeRepo := node.NewRepository(pool)
	nodeSvc := node.NewService(nodeRepo, logger)
	nodeMonitor := node.NewMonitor(nodeRepo, logger)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		nodeMonitor.Run(ctx)
	}()

	instanceRepo := instance.NewRepository(pool)
	instanceSvc := instance.NewService(instanceRepo)

	jobRepo := job.NewRepository(pool)
	jobSvc := job.NewService(jobRepo)
	jobPool := job.NewPool(jobRepo, logger, cfg.WorkerCount)
	jobPool.RegisterHandler(job.TypeCreateInstance, createInstanceJobHandler(instanceSvc))

	wg.Add(1)
	go func() {
		defer wg.Done()
		jobPool.Run(ctx)
	}()

	kafkaWriter := &kafka.Writer{
		Addr:        kafka.TCP(cfg.KafkaBrokers...),
		Balancer:    &kafka.LeastBytes{},
		ErrorLogger: kafkaErrorLogger(logger),
	}
	defer kafkaWriter.Close()

	kafkaReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.KafkaBrokers,
		Topic:   outbox.Topic,
		GroupID: "nebula-audit",
		// A fresh consumer group (no committed offset yet) starts from the
		// beginning of the topic rather than only new messages — an audit
		// trail must not silently skip events published before the
		// consumer happened to finish joining the group.
		StartOffset: kafka.FirstOffset,
		Logger:      kafkaDebugLogger(logger),
		ErrorLogger: kafkaErrorLogger(logger),
	})
	defer kafkaReader.Close()

	outboxRepo := outbox.NewRepository(pool)
	outboxPublisher := outbox.NewPublisher(outboxRepo, kafkaWriter, logger)

	wg.Add(1)
	go func() {
		defer wg.Done()
		outboxPublisher.Run(ctx)
	}()

	auditRepo := audit.NewRepository(pool)
	auditConsumer := audit.NewConsumer(auditRepo, kafkaReader, logger)

	wg.Add(1)
	go func() {
		defer wg.Done()
		auditConsumer.Run(ctx)
	}()

	srv := api.NewServer(":"+cfg.HTTPPort, pool, authSvc, tokens, nodeSvc, instanceSvc, jobSvc, logger)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("nebula-api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	wg.Wait()
	logger.Info("nebula-api stopped cleanly")
	return nil
}

func kafkaDebugLogger(logger *slog.Logger) kafka.LoggerFunc {
	return func(msg string, args ...interface{}) {
		logger.Debug(fmt.Sprintf(msg, args...))
	}
}

func kafkaErrorLogger(logger *slog.Logger) kafka.LoggerFunc {
	return func(msg string, args ...interface{}) {
		logger.Error(fmt.Sprintf(msg, args...))
	}
}

// createInstanceJobHandler mocks provisioning (spec section 60: "at this
// stage provisioning can still use mocks") by driving the instance state
// machine PENDING -> PROVISIONING -> RUNNING with a simulated delay. It does
// not touch the scheduler/node reservation — see the Phase 6 plan for why
// that's deliberately deferred to the provisioning saga phase.
func createInstanceJobHandler(instanceSvc instance.Service) job.Handler {
	return func(ctx context.Context, j job.Job) error {
		if j.InstanceID == nil {
			return fmt.Errorf("CREATE_INSTANCE job %s has no instance_id", j.ID)
		}
		var tenantID string
		if j.TenantID != nil {
			tenantID = *j.TenantID
		}

		if _, err := instanceSvc.Transition(ctx, tenantID, *j.InstanceID, instance.StatusProvisioning); err != nil {
			return fmt.Errorf("transition to PROVISIONING: %w", err)
		}

		select {
		case <-time.After(300 * time.Millisecond): // simulated provisioning work
		case <-ctx.Done():
			return ctx.Err()
		}

		if _, err := instanceSvc.Transition(ctx, tenantID, *j.InstanceID, instance.StatusRunning); err != nil {
			return fmt.Errorf("transition to RUNNING: %w", err)
		}
		return nil
	}
}
