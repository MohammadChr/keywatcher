package server

import (
	"embed"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"keywatcher/config"
	"keywatcher/internal/auth"
	"keywatcher/internal/handler"
	"keywatcher/internal/store"
)

//go:embed static/index.html
var staticFiles embed.FS

// responseWriterWrapper wraps http.ResponseWriter to track status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

type Server struct {
	cfg    *config.Config
	router *chi.Mux
	store  store.Store
}

func New(cfg *config.Config, s store.Store, authHandler *handler.AuthHandler, assetHandler *handler.AssetHandler, settingsHandler *handler.SettingsHandler, silenceHandler *handler.SilenceHandler) *Server {
	srv := &Server{cfg: cfg, router: chi.NewRouter(), store: s}

	srv.router.Use(middleware.RequestID)
	srv.router.Use(middleware.RealIP)

	// Custom panic recovery with no details exposure (Fix 5.2)
	srv.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error().Interface("panic", rec).Str("path", r.URL.Path).Msg("panic recovered")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error":"internal server error"}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	})

	// Remove headers that reveal infrastructure (Fix 1.3)
	srv.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Del("X-Powered-By")
			w.Header().Set("Server", "")
			next.ServeHTTP(w, r)
		})
	})

	// Add security headers (Fix 2.1)
	srv.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline'; "+
					"style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' blob: data:; "+
					"connect-src 'self'; "+
					"frame-ancestors 'none'")
			next.ServeHTTP(w, r)
		})
	})

	// CORS middleware (Fix 9.1)
	srv.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allowed := srv.cfg.AllowedOrigin
				if allowed == "" {
					allowed = "http://localhost:8080"
				}
				if origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					w.Header().Set("Access-Control-Max-Age", "3600")
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Apply setup check globally
	srv.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			skip := r.URL.Path == "/setup" ||
				strings.HasPrefix(r.URL.Path, "/setup/") ||
				r.URL.Path == "/healthz" ||
				r.URL.Path == "/readyz" ||
				r.URL.Path == "/metrics" ||
				r.URL.Path == "/api/v1/auth/methods" ||
				r.URL.Path == "/logo"
			if skip {
				next.ServeHTTP(w, r)
				return
			}
			done, err := s.IsSetupCompleted(r.Context())
			if err != nil || !done {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					w.Write([]byte(`{"error":"setup_required"}`))
					return
				}
				http.Redirect(w, r, "/setup", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Setup routes — always public, no auth, no setup check
	setupHandler := handler.NewSetupHandler(s, cfg)
	srv.router.Get("/setup/status", setupHandler.Status)

	// Rate limit setup endpoint (Fix 10.1)
	setupLimiter := auth.NewRateLimiter(3, 10*time.Minute)
	srv.router.With(setupLimiter.Middleware).Post("/setup", setupHandler.Complete)
	srv.router.Get("/setup", func(w http.ResponseWriter, r *http.Request) {
		data, _ := staticFiles.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
	srv.router.Get("/api/v1/auth/methods", setupHandler.AuthMethods)

	// Rate limiter for login (Fix 3.3)
	loginLimiter := auth.NewRateLimiter(5, time.Minute)

	// Public routes
	srv.router.With(loginLimiter.Middleware).Post("/api/v1/auth/login", authHandler.Login)
	srv.router.Post("/api/v1/auth/logout", authHandler.Logout)
	srv.router.Get("/api/v1/auth/oidc/login", authHandler.OIDCLogin)
	srv.router.Get("/api/v1/auth/oidc/callback", authHandler.OIDCCallback)

	// Logo — serve from disk (supports both GET and HEAD)
	logoHandler := func(w http.ResponseWriter, r *http.Request) {
		candidates := []struct{ path, mime string }{
			{"/assets/logo.png", "image/png"},
			{"/assets/logo.svg", "image/svg+xml"},
			{"assets/logo.png", "image/png"},
			{"assets/logo.svg", "image/svg+xml"},
		}
		for _, c := range candidates {
			data, err := os.ReadFile(c.path)
			if err == nil && len(data) > 0 {
				w.Header().Set("Content-Type", c.mime)
				w.Header().Set("Cache-Control", "public, max-age=3600")
				if r.Method != http.MethodHead {
					w.Write(data)
				}
				return
			}
		}
		http.NotFound(w, r)
	}
	srv.router.Get("/logo", logoHandler)
	srv.router.Head("/logo", logoHandler)

	// Protect /metrics endpoint (Fix 6.1)
	srv.router.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if srv.cfg.MetricsToken != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+srv.cfg.MetricsToken {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"metrics token required"}`))
				return
			}
		}
		promhttp.Handler().ServeHTTP(w, r)
	})

	srv.router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	srv.router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	userHandler := handler.NewUserHandler(s)

	// Public user creation (no auth required)
	srv.router.Post("/api/v1/users", userHandler.CreateUser)

	// Protected routes with auth middleware (must be before wildcard)
	authMiddleware := auth.RequireAuth(cfg.JWTSecret)

	// Asset routes - register ALL asset routes together, not split
	r := srv.router.With(authMiddleware)
	r.Get("/api/v1/assets", assetHandler.ListAssets)
	r.Get("/api/v1/assets/{id}", assetHandler.GetAsset)

	// Admin-only routes
	admin := srv.router.With(authMiddleware, auth.RequireAdmin)
	admin.Post("/api/v1/assets", assetHandler.CreateAsset)
	admin.Put("/api/v1/assets/{id}", assetHandler.UpdateAsset)
	admin.Delete("/api/v1/assets/{id}", assetHandler.DeleteAsset)
	admin.Get("/api/v1/users", userHandler.ListUsers)
	admin.Put("/api/v1/users/{id}/role", userHandler.UpdateRole)
	admin.Put("/api/v1/users/{id}/password", userHandler.ChangePassword)
	admin.Delete("/api/v1/users/{id}", userHandler.DeleteUser)
	admin.Put("/api/v1/users/{id}", userHandler.UpdateUser)
	admin.Get("/api/v1/settings", settingsHandler.Get)
	admin.Put("/api/v1/settings", settingsHandler.Update)
	admin.Get("/api/v1/settings/alerts", settingsHandler.GetAlerts)
	admin.Put("/api/v1/settings/alerts", settingsHandler.UpdateAlerts)
	admin.Post("/api/v1/settings/alerts/test", settingsHandler.TestAlert)
	admin.Get("/api/v1/silences", silenceHandler.List)
	admin.Post("/api/v1/assets/{id}/silence", silenceHandler.Silence)
	admin.Delete("/api/v1/assets/{id}/silence", silenceHandler.Unsilence)

	// Frontend SPA - serve index.html for root and SPA routes only
	srv.router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data, _ := staticFiles.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	return srv
}

func (s *Server) Router() http.Handler {
	return s.router
}
