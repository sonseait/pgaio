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
	settings  *handler.SettingsHandler
	queries   *handler.QueriesHandler

	vacuum     *handler.VacuumHandler
	locks      *handler.LocksHandler
	indexes    *handler.IndexesHandler
	extensions *handler.ExtensionsHandler
	alerts     *handler.AlertsHandler
	database   *handler.DatabaseHandler
	tuner      *handler.TunerHandler
	repack     *handler.RepackHandler
	totp       *service.TOTP
}

// New creates a new HTTP server with all routes.
func New(
	monitor *service.Monitor,
	walg *service.WalG,
	s3Client *service.S3Client,
	pgbouncer *service.PgBouncer,
	logPath string,
	pool *pgxpool.Pool,
	poolMgr *service.PoolManager,
	totpSvc *service.TOTP,
	configStore *service.ConfigStore,
	scheduler *service.Scheduler,
	alerter *service.Alerter,
) *Server {
	s := &Server{
		mux:       http.NewServeMux(),
		dashboard: handler.NewDashboardHandler(monitor),
		backup:    handler.NewBackupHandler(walg),
		s3:        handler.NewS3Handler(s3Client),
		pgbouncer: handler.NewPgBouncerHandler(pgbouncer),
		logs:      handler.NewLogHandler(logPath),
		config:    handler.NewConfigHandler(pool),
		overview:  handler.NewServerOverviewHandler(poolMgr),
		sql:       handler.NewSQLHandler(poolMgr),
		auth:      handler.NewAuthHandler(totpSvc),
		settings:  handler.NewSettingsHandler(configStore, scheduler),
		queries:   handler.NewQueriesHandler(poolMgr),

		vacuum:     handler.NewVacuumHandler(poolMgr),
		locks:      handler.NewLocksHandler(pool),
		indexes:    handler.NewIndexesHandler(poolMgr),
		extensions: handler.NewExtensionsHandler(poolMgr),
		alerts:     handler.NewAlertsHandler(alerter),
		database:   handler.NewDatabaseHandler(pool),
		tuner:      handler.NewTunerHandler(service.NewTuner(pool), pool, pgbouncer),
		repack:     handler.NewRepackHandler(poolMgr),
		totp:       totpSvc,
	}
	s.routes()
	return s
}

// protect wraps a handler with session middleware.
func (s *Server) protect(h http.HandlerFunc) http.HandlerFunc {
	return handler.SessionMiddleware(s.totp, h)
}

func (s *Server) routes() {
	// Auth (no session required)
	s.mux.HandleFunc("GET /api/auth/status", s.auth.GetStatus)
	s.mux.HandleFunc("GET /api/auth/setup", s.auth.GetSetup)
	s.mux.HandleFunc("POST /api/auth/setup/confirm", s.auth.ConfirmSetup)
	s.mux.HandleFunc("POST /api/auth/login", s.auth.Login)

	// Settings (GET = public, POST = TOTP)
	s.mux.HandleFunc("GET /api/settings", s.settings.GetSettings)
	s.mux.HandleFunc("POST /api/settings", s.protect(s.settings.UpdateSettings))
	s.mux.HandleFunc("GET /api/backups/schedule", s.settings.GetScheduleStatus)

	// Dashboard / Monitor
	s.mux.HandleFunc("GET /api/dashboard/stats", s.dashboard.GetStats)
	s.mux.HandleFunc("GET /api/dashboard/ws", s.dashboard.StreamStats)
	s.mux.HandleFunc("POST /api/dashboard/cancel/{pid}", s.protect(s.dashboard.CancelQuery))
	s.mux.HandleFunc("POST /api/dashboard/terminate/{pid}", s.protect(s.dashboard.TerminateBackend))

	// Backups
	s.mux.HandleFunc("GET /api/backups", s.backup.ListBackups)
	s.mux.HandleFunc("POST /api/backups/trigger", s.protect(s.backup.TriggerBackup))
	s.mux.HandleFunc("POST /api/backups/restore", s.protect(s.backup.RestoreBackup))

	// S3
	s.mux.HandleFunc("GET /api/s3/objects", s.s3.ListObjects)
	s.mux.HandleFunc("DELETE /api/s3/objects", s.protect(s.s3.DeleteObject))
	s.mux.HandleFunc("GET /api/s3/download", s.s3.GetDownloadURL)

	// PgBouncer
	s.mux.HandleFunc("GET /api/pgbouncer/stats", s.pgbouncer.GetStats)
	s.mux.HandleFunc("POST /api/pgbouncer/pause", s.protect(s.pgbouncer.Pause))
	s.mux.HandleFunc("POST /api/pgbouncer/resume", s.protect(s.pgbouncer.Resume))
	s.mux.HandleFunc("POST /api/pgbouncer/reload", s.protect(s.pgbouncer.Reload))
	s.mux.HandleFunc("POST /api/pgbouncer/kill", s.protect(s.pgbouncer.Kill))

	// Logs
	s.mux.HandleFunc("GET /api/logs", s.logs.GetRecentLogs)
	s.mux.HandleFunc("GET /api/logs/ws", s.logs.StreamLogs)

	// Config
	s.mux.HandleFunc("GET /api/config", s.config.GetConfig)
	s.mux.HandleFunc("POST /api/config/set", s.protect(s.config.SetConfig))
	s.mux.HandleFunc("POST /api/config/restart", s.protect(s.config.RestartPostgreSQL))

	// Server Overview
	s.mux.HandleFunc("GET /api/server/overview", s.overview.GetOverview)

	// SQL Editor
	s.mux.HandleFunc("POST /api/sql/execute", s.protect(s.sql.ExecuteSQL))
	s.mux.HandleFunc("GET /api/sql/snippets", s.sql.GetSnippets)
	s.mux.HandleFunc("GET /api/sql/history", s.sql.GetHistory)
	s.mux.HandleFunc("DELETE /api/sql/history", s.protect(s.sql.ClearHistory))
	s.mux.HandleFunc("GET /api/sql/schema", s.sql.GetSchema)

	// Slow Queries + Explain
	s.mux.HandleFunc("GET /api/queries/slow", s.queries.GetSlowQueries)
	s.mux.HandleFunc("POST /api/queries/explain", s.protect(s.queries.ExplainQuery))
	s.mux.HandleFunc("POST /api/queries/reset", s.protect(s.queries.ResetStats))

	// Vacuum + Bloat
	s.mux.HandleFunc("GET /api/vacuum/stats", s.vacuum.GetVacuumStats)
	s.mux.HandleFunc("GET /api/vacuum/bloat", s.vacuum.GetBloatStats)
	s.mux.HandleFunc("POST /api/vacuum/trigger", s.protect(s.vacuum.TriggerVacuum))

	// Repack (online table compaction)
	s.mux.HandleFunc("GET /api/repack/tables", s.repack.GetTables)
	s.mux.HandleFunc("POST /api/repack/run", s.protect(s.repack.Run))
	s.mux.HandleFunc("GET /api/repack/status", s.repack.GetStatus)
	s.mux.HandleFunc("POST /api/repack/cancel", s.protect(s.repack.CancelRepack))

	// Locks
	s.mux.HandleFunc("GET /api/locks", s.locks.GetLocks)

	// Index Advisor
	s.mux.HandleFunc("GET /api/indexes/advice", s.indexes.GetIndexAdvice)

	// Extensions
	s.mux.HandleFunc("GET /api/extensions", s.extensions.ListExtensions)
	s.mux.HandleFunc("POST /api/extensions/install", s.protect(s.extensions.InstallExtension))
	s.mux.HandleFunc("POST /api/extensions/uninstall", s.protect(s.extensions.UninstallExtension))

	// Database Export/Import
	s.mux.HandleFunc("GET /api/database/export", s.protect(s.database.ExportDatabase))
	s.mux.HandleFunc("POST /api/database/import", s.protect(s.database.ImportDatabase))
	s.mux.HandleFunc("GET /api/database/list", s.database.ListDatabases)

	// Alerts
	s.mux.HandleFunc("GET /api/alerts", s.alerts.GetAlerts)
	s.mux.HandleFunc("POST /api/alerts/test", s.protect(s.alerts.TestAlert))

	// DB Tuner Wizard
	s.mux.HandleFunc("GET /api/tuner/system", s.tuner.GetSystemInfo)
	s.mux.HandleFunc("POST /api/tuner/analyze", s.tuner.Analyze)
	s.mux.HandleFunc("POST /api/tuner/apply", s.protect(s.tuner.Apply))

	// Static files (embedded frontend)
	staticFS, err := fs.Sub(web.StaticFiles, "static")
	if err != nil {
		log.Fatal("failed to create sub filesystem:", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))

	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			_, err := fs.Stat(staticFS, r.URL.Path[1:])
			if err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Session-ID")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	log.Printf("[%s] %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}
