package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/job"
	"github.com/milosursulovic/nebula/internal/network"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/provisioning"
)

// NewServer builds the nebula-api HTTP server: router, middleware, and routes.
func NewServer(addr string, db Pinger, authSvc auth.Service, tokens auth.TokenIssuer, nodeSvc node.Service, instanceSvc instance.Service, jobSvc job.Service, networkSvc network.Service, deleteVM provisioning.VMDeleter, releaseIP provisioning.NetworkDeleter, logger *slog.Logger, nodeBootstrapSecret string) *http.Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))

	r.Get("/health", handleHealth)
	r.Get("/ready", handleReady(db))

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", handleRegister(authSvc))
			r.Post("/login", handleLogin(authSvc))
			r.Post("/refresh", handleRefresh(authSvc))
			r.Post("/logout", handleLogout(authSvc))
		})

		r.With(auth.Authenticate(tokens)).Get("/me", handleMe)

		r.Route("/nodes", func(r chi.Router) {
			// Heartbeat authenticates with the node's own bearer token
			// (checked inside the handler), not a user JWT.
			r.Post("/{id}/heartbeat", handleHeartbeat(nodeSvc))

			// Registration accepts the bootstrap secret (unattended agents)
			// or a SUPER_ADMIN JWT (manual/human registration) — see
			// requireNodeBootstrapOrSuperAdmin's doc comment.
			r.With(requireNodeBootstrapOrSuperAdmin(tokens, nodeBootstrapSecret)).
				Post("/register", handleRegisterNode(nodeSvc))

			r.Group(func(r chi.Router) {
				r.Use(auth.Authenticate(tokens))
				r.Use(auth.RequireRole(auth.RoleSuperAdmin))

				r.Get("/", handleListNodes(nodeSvc))
				r.Get("/{id}", handleGetNode(nodeSvc))
			})
		})

		r.Route("/instances", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))

			r.Post("/", handleCreateInstance(instanceSvc))
			r.Get("/", handleListInstances(instanceSvc))
			r.Get("/{id}", handleGetInstance(instanceSvc))
			r.Delete("/{id}", handleDeleteInstance(instanceSvc, nodeSvc, deleteVM, releaseIP, logger))
		})

		r.Route("/networks", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(auth.RequireRole(auth.RoleSuperAdmin))

			r.Post("/", handleCreateNetwork(networkSvc))
			r.Get("/", handleListNetworks(networkSvc))
			r.Get("/{id}", handleGetNetwork(networkSvc))
		})

		r.Route("/jobs", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(auth.RequireRole(auth.RoleSuperAdmin))

			r.Get("/", handleListJobs(jobSvc))
			r.Get("/{id}", handleGetJob(jobSvc))
			r.Post("/{id}/retry", handleRetryJob(jobSvc))
		})
	})

	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
