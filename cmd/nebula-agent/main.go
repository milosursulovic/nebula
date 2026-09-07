package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/milosursulovic/nebula/internal/agent"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("nebula-agent exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := agent.Load()
	if err != nil {
		return err
	}

	store := agent.NewStore()
	registrar := agent.NewRegistrar(cfg, store, logger)

	if err := registrar.Register(ctx); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		registrar.RunHeartbeatLoop(ctx)
	}()

	srv := agent.NewServer(":"+cfg.Port, store)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("nebula-agent listening", "addr", srv.Addr, "node_id", registrar.NodeID())
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
	logger.Info("nebula-agent stopped cleanly")
	return nil
}
