package server

import (
	"io/fs"
	"log"
	"net/http"

	"pgaio/handler"
	"pgaio/service"
	"pgaio/web"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds all HTTP handlers and services.
type Server struct {
	mux *http.ServeMux

	dashboard *handler.DashboardHandler
	backup    *handler.BackupHandler
	s3        *handler.S3Handler
	pgbouncer *handler.PgBouncerHandler
	logs      *handler.LogHandler
	config    *handler.ConfigHandler
	overview  *handler.ServerOverviewHandler
	sql       *handler.SQLHandler
	auth      *handler.AuthHandler
	totp      *service.TOTP
}

// New creates a new HTTP server with all routes.
func New(
	monitor *service.Monitor,
	walg *service.WalG,
	s3Client *service.S3Client,
	pgbouncer *service.PgBouncer,
	logPath string,
	pool *pgxpool.Pool,
	totpSvc *service.TOTP,
) *Server {
	s := &Server{
		mux:       http.NewServeMux(),
		dashboard: handler.NewDashboardHandler(monitor),
		backup:    handler.NewBackupHandler(walg),
		s3:        handler.NewS3Handler(s3Client),
		pgbouncer: handler.NewPgBouncerHandler(pgbouncer),
		logs:      handler.NewLogHandler(logPath),
		config:    handler.NewConfigHandler(pool),
		overview:  handler.NewServerOverviewHandler(pool),
		sql:       handler.NewSQLHandler(pool),
		auth:      handler.NewAuthHandler(totpSvc),
		totp:      totpSvc,
	}
	s.routes()
	return s
}

// protect wraps a handler with TOTP middleware.
func (s *Server) protect(h http.HandlerFunc) http.HandlerFunc {
	return handler.TOTPMiddleware(s.totp, h)
}

func (s *Server) routes() {
	// Auth (no TOTP required)
	s.mux.HandleFunc("GET /api/auth/setup", s.auth.GetSetup)
	s.mux.HandleFunc("POST /api/auth/verify", s.auth.Verify)

	// Dashboard / Monitor (GET = no TOTP, POST = TOTP)
	s.mux.HandleFunc("GET /api/dashboard/stats", s.dashboard.GetStats)
	s.mux.HandleFunc("GET /api/dashboard/ws", s.dashboard.StreamStats)
	s.mux.HandleFunc("POST /api/dashboard/cancel/{pid}", s.protect(s.dashboard.CancelQuery))
	s.mux.HandleFunc("POST /api/dashboard/terminate/{pid}", s.protect(s.dashboard.TerminateBackend))

	// Backups (GET = no TOTP, POST = TOTP)
	s.mux.HandleFunc("GET /api/backups", s.backup.ListBackups)
	s.mux.HandleFunc("POST /api/backups/trigger", s.protect(s.backup.TriggerBackup))
	s.mux.HandleFunc("POST /api/backups/restore", s.protect(s.backup.RestoreBackup))

	// S3 (GET = no TOTP, DELETE = TOTP)
	s.mux.HandleFunc("GET /api/s3/objects", s.s3.ListObjects)
	s.mux.HandleFunc("DELETE /api/s3/objects", s.protect(s.s3.DeleteObject))
	s.mux.HandleFunc("GET /api/s3/download", s.s3.GetDownloadURL)

	// PgBouncer (GET = no TOTP, POST = TOTP)
	s.mux.HandleFunc("GET /api/pgbouncer/stats", s.pgbouncer.GetStats)
	s.mux.HandleFunc("POST /api/pgbouncer/pause", s.protect(s.pgbouncer.Pause))
	s.mux.HandleFunc("POST /api/pgbouncer/resume", s.protect(s.pgbouncer.Resume))
	s.mux.HandleFunc("POST /api/pgbouncer/reload", s.protect(s.pgbouncer.Reload))
	s.mux.HandleFunc("POST /api/pgbouncer/kill", s.protect(s.pgbouncer.Kill))

	// Logs (read-only, no TOTP)
	s.mux.HandleFunc("GET /api/logs", s.logs.GetRecentLogs)
	s.mux.HandleFunc("GET /api/logs/ws", s.logs.StreamLogs)

	// Config (read-only, no TOTP)
	s.mux.HandleFunc("GET /api/config", s.config.GetConfig)

	// Server Overview (read-only, no TOTP)
	s.mux.HandleFunc("GET /api/server/overview", s.overview.GetOverview)

	// SQL (POST = TOTP)
	s.mux.HandleFunc("POST /api/sql/execute", s.protect(s.sql.ExecuteSQL))
	s.mux.HandleFunc("GET /api/sql/snippets", s.sql.GetSnippets)

	// Static files (embedded frontend)
	staticFS, err := fs.Sub(web.StaticFiles, "static")
	if err != nil {
		log.Fatal("failed to create sub filesystem:", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))

	// Serve index.html for SPA routes, static files otherwise
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Try serving static file first
		if r.URL.Path != "/" {
			// Check if file exists in embedded FS
			_, err := fs.Stat(staticFS, r.URL.Path[1:]) // Remove leading /
			if err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Fall back to index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-TOTP-Code")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Request logging
	log.Printf("[%s] %s", r.Method, r.URL.Path)

	s.mux.ServeHTTP(w, r)
}
