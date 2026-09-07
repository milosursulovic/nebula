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

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/common"
	"github.com/milosursulovic/nebula/internal/node"
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

	srv := api.NewServer(":"+cfg.HTTPPort, pool, authSvc, tokens, nodeSvc, logger)

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
