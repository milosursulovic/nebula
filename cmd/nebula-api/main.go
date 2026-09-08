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
	"github.com/milosursulovic/nebula/internal/network"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/outbox"
	"github.com/milosursulovic/nebula/internal/provisioning"
	"github.com/milosursulovic/nebula/internal/scheduler"
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

	networkRepo := network.NewRepository(pool)
	networkSvc := network.NewService(networkRepo)

	schedulerSvc, err := scheduler.NewScheduler(cfg.SchedulerStrategy, nodeRepo)
	if err != nil {
		return err
	}
	mockSteps := provisioning.NewMockSteps(logger)
	networkSteps := provisioning.NewNetworkSteps(networkSvc)
	agentSteps, err := provisioning.NewAgentSteps(nodeSvc, cfg.AgentPort, cfg.AgentTLSCAFile, logger)
	if err != nil {
		return err
	}
	saga := provisioning.NewSagaWithSteps(instanceSvc, nodeSvc, schedulerSvc, logger, provisioning.Steps{
		CreateDisk: mockSteps.CreateDisk, DeleteDisk: mockSteps.DeleteDisk,
		CreateNetwork: networkSteps.CreateNetwork, DeleteNetwork: networkSteps.DeleteNetwork,
		CreateVM: agentSteps.CreateVM, DeleteVM: agentSteps.DeleteVM, StartVM: agentSteps.StartVM,
	})

	jobRepo := job.NewRepository(pool)
	jobSvc := job.NewService(jobRepo)
	jobPool := job.NewPool(jobRepo, logger, cfg.WorkerCount)
	jobPool.RegisterHandler(job.TypeCreateInstance, createInstanceJobHandler(saga))

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

	srv := api.NewServer(":"+cfg.HTTPPort, pool, authSvc, tokens, nodeSvc, instanceSvc, jobSvc, networkSvc, agentSteps.DeleteVM, networkSteps.DeleteNetwork, logger, cfg.NodeBootstrapSecret)

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

// createInstanceJobHandler adapts a CREATE_INSTANCE job to the provisioning
// saga (spec section 62): schedule a node, reserve resources, create disk,
// create network, create VM, start VM — compensating in reverse on failure.
func createInstanceJobHandler(saga *provisioning.Saga) job.Handler {
	return func(ctx context.Context, j job.Job) error {
		if j.InstanceID == nil {
			return fmt.Errorf("CREATE_INSTANCE job %s has no instance_id", j.ID)
		}
		var tenantID string
		if j.TenantID != nil {
			tenantID = *j.TenantID
		}

		return saga.Provision(ctx, tenantID, *j.InstanceID)
	}
}
