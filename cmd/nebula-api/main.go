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

	"github.com/prometheus/client_golang/prometheus"
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
	"github.com/milosursulovic/nebula/internal/storage"
	"github.com/milosursulovic/nebula/internal/tracing"
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

	tp, err := tracing.NewProvider(ctx, "nebula-api", cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			logger.Error("tracer provider shutdown failed", "error", err)
		}
	}()

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
	prometheus.MustRegister(instance.NewMetricsCollector(instanceRepo, logger))

	networkRepo := network.NewRepository(pool)
	networkSvc := network.NewService(networkRepo)

	schedulerSvc, err := scheduler.NewScheduler(cfg.SchedulerStrategy, nodeRepo)
	if err != nil {
		return err
	}
	networkSteps := provisioning.NewNetworkSteps(networkSvc)
	agentSteps, err := provisioning.NewAgentSteps(nodeSvc, cfg.AgentPort, cfg.AgentTLSCAFile, logger)
	if err != nil {
		return err
	}

	storageRepo := storage.NewRepository(pool)
	storageSvc := storage.NewService(storageRepo, agentSteps)
	diskSteps := provisioning.NewDiskSteps(storageSvc)

	saga := provisioning.NewSagaWithSteps(instanceSvc, nodeSvc, schedulerSvc, logger, provisioning.Steps{
		CreateDisk: diskSteps.CreateDisk, DeleteDisk: diskSteps.DeleteDisk,
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
		Addr:     kafka.TCP(cfg.KafkaBrokers...),
		Balancer: &kafka.LeastBytes{},
		// kafka.Writer's own BatchTimeout defaults to 1s (batches up
		// writes arriving within that window into one request) — but
		// outbox.Publisher already does its own batching one level up
		// (ticks once a second, gathers up to PublisherBatchSize events,
		// then calls WriteMessages once per event in that batch), so this
		// second, redundant 1s timer only adds latency: a Publish call
		// with nothing else queued right behind it just waits out the
		// full second for no reason. Phase 17's benchmarking measured
		// this directly (~1s/op with the default, unbatched calls)
		// before setting this.
		BatchTimeout: 10 * time.Millisecond,
		ErrorLogger:  kafkaErrorLogger(logger),
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
	prometheus.MustRegister(&kafkaLagCollector{reader: kafkaReader})

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

	srv := api.NewServer(":"+cfg.HTTPPort, pool, authSvc, tokens, nodeSvc, instanceSvc, jobSvc, networkSvc, storageSvc, agentSteps.DeleteVM, networkSteps.DeleteNetwork, agentSteps.StartVM, agentSteps.StopVM, logger, cfg.NodeBootstrapSecret)

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

var kafkaConsumerLagDesc = prometheus.NewDesc(
	"nebula_kafka_consumer_lag",
	"Lag (unread messages) for the nebula-audit consumer group, spec section 35.",
	nil, nil,
)

// kafkaLagCollector is a live prometheus.Collector wrapping the audit
// consumer's *kafka.Reader — queried at scrape time (kafka-go's Stats() is
// cheap/synchronous) rather than pushed, same idiom as
// instance.metricsCollector.
type kafkaLagCollector struct {
	reader *kafka.Reader
}

func (c *kafkaLagCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- kafkaConsumerLagDesc
}

func (c *kafkaLagCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.reader.Stats()
	ch <- prometheus.MustNewConstMetric(kafkaConsumerLagDesc, prometheus.GaugeValue, float64(stats.Lag))
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
