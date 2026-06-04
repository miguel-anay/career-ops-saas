package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"github.com/miguel-anay/career-ops-saas/api/internal/auth"
	"github.com/miguel-anay/career-ops-saas/api/internal/companies"
	"github.com/miguel-anay/career-ops-saas/api/internal/config"
	"github.com/miguel-anay/career-ops-saas/api/internal/cv"
	"github.com/miguel-anay/career-ops-saas/api/internal/evaluate"
	"github.com/miguel-anay/career-ops-saas/api/internal/jobs"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/miguel-anay/career-ops-saas/api/internal/platform"
	"github.com/miguel-anay/career-ops-saas/api/internal/scan"
	"github.com/miguel-anay/career-ops-saas/api/internal/tracker"
	"github.com/miguel-anay/career-ops-saas/api/internal/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Load .env file in development (ignored in production where env vars are injected).
	_ = godotenv.Load()

	// 1. Load config.
	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "error", err)
		os.Exit(1)
	}

	// 2. Connect pgxpool.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := platform.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres connect failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// 3. Init R2 client (optional — skip if not configured).
	var r2Client *platform.R2Client
	if cfg.R2AccountID != "" {
		r2Client, err = platform.NewR2Client(cfg)
		if err != nil {
			logger.Warn("R2 client init failed — file operations will be unavailable", "error", err)
		}
	}
	_ = r2Client

	// 4. Create chi router.
	r := chi.NewRouter()

	// 5. Global middleware: recover, logging, cors.
	r.Use(middleware.Recoverer(logger))
	r.Use(middleware.Logger(logger))
	r.Use(middleware.CORS(cfg.WebOrigin))
	r.Use(chiMiddleware.StripSlashes)

	// 6. Mount /auth routes (no auth middleware).
	authHandler := auth.NewHandler(cfg, pool)
	r.Group(func(r chi.Router) {
		r.Get("/auth/google", authHandler.GoogleLogin)
		r.Get("/auth/google/callback", authHandler.GoogleCallback)
		r.Post("/auth/refresh", authHandler.Refresh)
		r.Post("/auth/logout", authHandler.Logout)
	})

	// 7. Mount /api routes with auth + tenant middleware.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Authenticator(cfg.JWTSecret))
		r.Use(middleware.TenantIsolation(pool))

		r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})

		// T-26: Jobs domain.
		jobsHandler := jobs.NewHandler(jobs.NewService(pool))
		jobsHandler.RegisterRoutes(r)

		// T-29: Companies domain.
		companiesHandler := companies.NewHandler(companies.NewService(pool))
		companiesHandler.RegisterRoutes(r)

		// T-32: Scan domain.
		scanHandler := scan.NewHandler(scan.NewService(pool))
		scanHandler.RegisterRoutes(r)

		// T-35: Evaluate domain.
		evaluateHandler := evaluate.NewHandler(evaluate.NewService(pool))
		evaluateHandler.RegisterRoutes(r)

		// T-37: CV domain.
		cvHandler := cv.NewHandler(cv.NewService(pool), r2Client)
		cvHandler.RegisterRoutes(r)

		// T-38: Tracker domain.
		trackerHandler := tracker.NewHandler(tracker.NewService(pool))
		trackerHandler.RegisterRoutes(r)
	})

	// 8. WebSocket hub + listener (T-39..T-43).
	hub := ws.NewHub()
	go hub.Run(context.Background())

	if err := ws.StartListener(context.Background(), pool, hub); err != nil {
		logger.Error("ws listener start failed", "error", err)
		// Non-fatal: WS will not deliver real-time updates but API still works.
	}

	// Mount /ws routes with auth via query param.
	r.Get("/ws/scan", ws.ScanProgressHandler(hub, cfg.JWTSecret))

	// 9. Start HTTP server.
	srv := &http.Server{
		Addr:         cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 10. Graceful shutdown on SIGTERM/SIGINT.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		logger.Info("server starting", "addr", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	logger.Info("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}

	logger.Info("server stopped")
}
