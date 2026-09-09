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
	"github.com/milosursulovic/nebula/internal/ratelimit"
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

	// Phase 18/spec section 40's exact shutdown sequence (stop requests ->
	// stop Kafka consumers -> stop workers -> flush telemetry -> close
	// database) needs each stage independently stoppable, in that order —
	// a single shared ctx (as every background loop used before this)
	// cancels everything at once on a signal, not sequenced. workerCtx
	// covers the job pool + node monitor; kafkaCtx (below) covers the
	// outbox publisher + audit consumer.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	var wgWorkers sync.WaitGroup

	nodeRepo := node.NewRepository(pool)
	nodeSvc := node.NewService(nodeRepo, logger)
	nodeMonitor := node.NewMonitor(nodeRepo, logger)

	wgWorkers.Add(1)
	go func() {
		defer wgWorkers.Done()
		nodeMonitor.Run(workerCtx)
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
	agentSteps, err := provisioning.NewAgentSteps(nodeSvc, cfg.AgentPort, cfg.AgentTLSCAFile, cfg.AgentClientTLSCertFile, cfg.AgentClientTLSKeyFile, logger)
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

	wgWorkers.Add(1)
	go func() {
		defer wgWorkers.Done()
		jobPool.Run(workerCtx)
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

	kafkaCtx, cancelKafka := context.WithCancel(context.Background())
	defer cancelKafka()
	var wgKafka sync.WaitGroup

	outboxRepo := outbox.NewRepository(pool)
	outboxPublisher := outbox.NewPublisher(outboxRepo, kafkaWriter, logger)

	wgKafka.Add(1)
	go func() {
		defer wgKafka.Done()
		outboxPublisher.Run(kafkaCtx)
	}()

	auditRepo := audit.NewRepository(pool)
	auditConsumer := audit.NewConsumer(auditRepo, kafkaReader, logger)

	wgKafka.Add(1)
	go func() {
		defer wgKafka.Done()
		auditConsumer.Run(kafkaCtx)
	}()

	limiter := ratelimit.NewRedisLimiter(cfg.RedisAddr)

	srv := api.NewServer(":"+cfg.HTTPPort, pool, authSvc, tokens, nodeSvc, instanceSvc, jobSvc, networkSvc, storageSvc, agentSteps.DeleteVM, networkSteps.DeleteNetwork, agentSteps.StartVM, agentSteps.StopVM, limiter, cfg.TLSCertFile != "", logger, cfg.NodeBootstrapSecret)

	errCh := make(chan error, 1)
	go func() {
		// TLS stays opt-in (Phase 18): unset by default, so every
		// existing plain-HTTP curl example and the CLI's default URL
		// keep working unchanged. Set both env vars to serve HTTPS
		// instead — see deployments/certs/README.md.
		if cfg.TLSCertFile != "" {
			logger.Info("nebula-api listening", "addr", srv.Addr, "tls", true)
			if err := srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
			return
		}
		logger.Info("nebula-api listening", "addr", srv.Addr, "tls", false)
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

	// Spec section 40's exact sequence. srv/pool/kafkaWriter/kafkaReader/tp
	// also have `defer`d closes above as a safety net for any early-return
	// error path before this point ever runs — all four are safe to close
	// twice (idempotent), so calling them explicitly here too, in order,
	// costs nothing and is what makes the log sequence below actually
	// true rather than "whatever order Go's defer stack happens to close
	// things in."
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("http server stopped")

	cancelKafka()
	wgKafka.Wait()
	logger.Info("kafka consumers stopped")

	cancelWorkers()
	wgWorkers.Wait()
	logger.Info("workers stopped")

	if err := tp.Shutdown(shutdownCtx); err != nil {
		logger.Error("tracer provider shutdown failed", "error", err)
	} else {
		logger.Info("telemetry flushed")
	}

	kafkaWriter.Close()
	kafkaReader.Close()
	pool.Close()
	logger.Info("database closed")

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
