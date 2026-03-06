package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pgaio/server"
	"pgaio/service"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("🚀 Starting PGAIO Backend...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// PostgreSQL connection
	dbURL := getEnv("DATABASE_URL", "")
	if dbURL == "" {
		// Build from individual vars (bitnami style)
		host := getEnv("PGHOST", "/tmp")
		port := getEnv("PGPORT", "5432")
		user := getEnv("POSTGRESQL_USERNAME", "postgres")
		pass := getEnv("POSTGRESQL_PASSWORD", "postgres")
		dbname := getEnv("POSTGRESQL_DATABASE", "postgres")
		dbURL = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, pass, dbname)
	}

	poolConfig, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("failed to parse database URL: %v", err)
	}
	poolConfig.MaxConns = 5
	poolConfig.MinConns = 1

	// Retry connection to wait for PostgreSQL to start
	var pool *pgxpool.Pool
	for i := 0; i < 30; i++ {
		pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				break
			}
			pool.Close()
		}
		log.Printf("waiting for PostgreSQL... (%d/30)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("failed to connect to PostgreSQL after 30 retries: %v", err)
	}
	defer pool.Close()
	log.Println("✅ Connected to PostgreSQL")

	// Services
	monitor := service.NewMonitor(pool)
	walg := service.NewWalG(getEnv("PGDATA", "/bitnami/postgresql/data"))

	// S3 client (optional)
	var s3Client *service.S3Client
	s3Prefix := getEnv("WALG_S3_PREFIX", "")
	if s3Prefix != "" {
		bucket, _ := service.ParseWalgS3Prefix(s3Prefix)
		endpoint := getEnv("AWS_ENDPOINT", getEnv("AWS_S3_ENDPOINT", ""))
		accessKey := getEnv("AWS_ACCESS_KEY_ID", "")
		secretKey := getEnv("AWS_SECRET_ACCESS_KEY", "")
		region := getEnv("AWS_REGION", getEnv("AWS_DEFAULT_REGION", "us-east-1"))
		useSSL := getEnv("AWS_USE_SSL", "false") == "true"

		s3Client, err = service.NewS3Client(endpoint, accessKey, secretKey, bucket, region, useSSL)
		if err != nil {
			log.Printf("⚠️  S3 client init failed (S3 features disabled): %v", err)
		} else {
			log.Printf("✅ S3 connected (bucket: %s)", bucket)
		}
	} else {
		log.Println("ℹ️  S3 not configured (WALG_S3_PREFIX not set)")
	}

	// PgBouncer admin (optional)
	var pgbouncer *service.PgBouncer
	pgbAddr := getEnv("PGBOUNCER_ADMIN_ADDR", "127.0.0.1:6432")
	pgbUser := getEnv("PGBOUNCER_ADMIN_USER", "pgbouncer")
	pgbPass := getEnv("POSTGRESQL_PASSWORD", "postgres")
	pgbouncer = service.NewPgBouncer(pgbAddr, pgbUser, pgbPass)
	log.Printf("✅ PgBouncer admin configured (%s)", pgbAddr)

	// Log file path
	logPath := getEnv("PG_LOG_PATH", "/opt/bitnami/postgresql/logs/postgresql.log")
	log.Printf("✅ Log file: %s", logPath)

	// TOTP authentication
	totpSvc := service.NewTOTP()

	// Config store (persistent JSON settings)
	configStore := service.NewConfigStore()

	// Backup scheduler
	scheduler := service.NewScheduler(walg, configStore)

	// Alerter (health checks + Telegram)
	alerter := service.NewAlerter(configStore, monitor, walg)

	// HTTP Server
	srv := server.New(monitor, walg, s3Client, pgbouncer, logPath, pool, totpSvc, configStore, scheduler, alerter)
	addr := ":" + getEnv("PGAIO_PORT", "8080")
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("🛑 Shutting down...")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		httpServer.Shutdown(shutCtx)
	}()

	log.Printf("🌐 PGAIO Web UI listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
