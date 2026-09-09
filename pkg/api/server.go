package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/milosursulovic/nebula/internal/auth"
	"github.com/milosursulovic/nebula/internal/instance"
	"github.com/milosursulovic/nebula/internal/job"
	"github.com/milosursulovic/nebula/internal/metrics"
	"github.com/milosursulovic/nebula/internal/network"
	"github.com/milosursulovic/nebula/internal/node"
	"github.com/milosursulovic/nebula/internal/provisioning"
	"github.com/milosursulovic/nebula/internal/ratelimit"
	"github.com/milosursulovic/nebula/internal/storage"
)

// NewServer builds the nebula-api HTTP server: router, middleware, and
// routes. tlsEnabled only affects which headers get set (Strict-
// Transport-Security is meaningless, and wrong, to send over plain HTTP)
// — the caller (cmd/nebula-api/main.go) decides whether to actually call
// ListenAndServe or ListenAndServeTLS based on the same config.
func NewServer(addr string, db Pinger, authSvc auth.Service, tokens auth.TokenIssuer, nodeSvc node.Service, instanceSvc instance.Service, jobSvc job.Service, networkSvc network.Service, storageSvc storage.Service, deleteVM provisioning.VMDeleter, releaseIP provisioning.NetworkDeleter, startVM provisioning.VMStarter, stopVM provisioning.VMStopper, limiter ratelimit.Limiter, tlsEnabled bool, logger *slog.Logger, nodeBootstrapSecret string) *http.Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))
	r.Use(securityHeaders(tlsEnabled))

	r.Get("/health", handleHealth)
	r.Get("/ready", handleReady(db))
	// Unauthenticated, same convention as /health and /ready — Prometheus
	// scrapes this directly, no app-level auth (spec section 35).
	r.Handle("/metrics", promhttp.Handler())

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
				r.Use(rateLimit(limiter))
				r.Use(auth.RequireRole(auth.RoleSuperAdmin))

				r.Get("/", handleListNodes(nodeSvc))
				r.Get("/{id}", handleGetNode(nodeSvc))
				r.Post("/{id}/drain", handleDrainNode(nodeSvc))
			})
		})

		r.Route("/tenants", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(rateLimit(limiter))
			r.Use(auth.RequireRole(auth.RoleSuperAdmin))

			r.Get("/", handleListTenants(authSvc))
		})

		r.Route("/instances", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(rateLimit(limiter))

			r.Post("/", handleCreateInstance(instanceSvc))
			r.Get("/", handleListInstances(instanceSvc))
			r.Get("/{id}", handleGetInstance(instanceSvc))
			r.Delete("/{id}", handleDeleteInstance(instanceSvc, nodeSvc, storageSvc, deleteVM, releaseIP, logger))
			r.Post("/{id}/start", handleStartInstance(instanceSvc, startVM, logger))
			r.Post("/{id}/stop", handleStopInstance(instanceSvc, stopVM, logger))

			r.Post("/{id}/disks", handleCreateDisk(instanceSvc, storageSvc))
			r.Get("/{id}/disks", handleListInstanceDisks(instanceSvc, storageSvc))
		})

		r.Route("/disks", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(rateLimit(limiter))

			r.Get("/{id}", handleGetDisk(storageSvc))
			r.Delete("/{id}", handleDeleteDisk(storageSvc))
			r.Post("/{id}/attach", handleAttachDisk(instanceSvc, storageSvc))
			r.Post("/{id}/detach", handleDetachDisk(storageSvc))
			r.Post("/{id}/resize", handleResizeDisk(storageSvc))
		})

		r.Route("/networks", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(rateLimit(limiter))
			r.Use(auth.RequireRole(auth.RoleSuperAdmin))

			r.Post("/", handleCreateNetwork(networkSvc))
			r.Get("/", handleListNetworks(networkSvc))
			r.Get("/{id}", handleGetNetwork(networkSvc))
		})

		r.Route("/jobs", func(r chi.Router) {
			r.Use(auth.Authenticate(tokens))
			r.Use(rateLimit(limiter))
			r.Use(auth.RequireRole(auth.RoleSuperAdmin))

			r.Get("/", handleListJobs(jobSvc))
			r.Get("/{id}", handleGetJob(jobSvc))
			r.Post("/{id}/retry", handleRetryJob(jobSvc))
		})
	})

	// otelhttp wraps the whole router (spec section 36) — one root span
	// per request, named by method+matched-route-pattern once chi has
	// resolved it, with context propagation set up automatically so any
	// span created deeper in the call stack (DB, job enqueue, etc.)
	// nests under it.
	handler := otelhttp.NewHandler(r, "nebula-api",
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return r.Method + " " + routePattern(r)
		}),
	)

	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// rateLimitUserPerMinute / rateLimitTenantPerMinute are spec section 37's
// own worked example numbers.
const (
	rateLimitUserPerMinute   = 100
	rateLimitTenantPerMinute = 1000
)

// rateLimit enforces spec section 37's per-user and per-tenant request
// limits — applied inside each already-authenticated route group (after
// auth.Authenticate, so identity is available), never at the top level:
// chi middleware order means a router-wide r.Use would run before any
// route-group's own auth.Authenticate, where there's no identity yet.
// Per-IP limiting is spec's own "Eventually" item, not built.
func rateLimit(limiter ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := auth.IdentityFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			userOK, err := limiter.Allow(r.Context(), "ratelimit:user:"+identity.UserID, rateLimitUserPerMinute, time.Minute)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "rate limit check failed")
				return
			}
			tenantOK, err := limiter.Allow(r.Context(), "ratelimit:tenant:"+identity.TenantID, rateLimitTenantPerMinute, time.Minute)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "rate limit check failed")
				return
			}
			if !userOK || !tenantOK {
				writeError(w, r, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "too many requests, slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// securityHeaders sets standard hardening headers on every response.
// Strict-Transport-Security is only meaningful (and only correct) when
// actually serving over TLS — sending it over plain HTTP would tell
// browsers to force HTTPS for a server that doesn't have it.
func securityHeaders(tlsEnabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if tlsEnabled {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// routePattern returns chi's matched route pattern (e.g.
// "/api/v1/instances/{id}"), not the raw request path — used for the
// Prometheus path label and trace span name so per-ID paths don't
// explode cardinality. Only valid to call after chi has finished routing
// (i.e. after the inner handler has run).
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start)

			path := routePattern(r)
			status := strconv.Itoa(ww.Status())
			metrics.APIRequestsTotal.WithLabelValues(r.Method, path, status).Inc()
			metrics.APIRequestDuration.WithLabelValues(r.Method, path).Observe(duration.Seconds())

			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", duration.Milliseconds(),
			)
		})
	}
}
