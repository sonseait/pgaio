package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DatabaseHandler struct {
	pool *pgxpool.Pool
}

func NewDatabaseHandler(pool *pgxpool.Pool) *DatabaseHandler {
	return &DatabaseHandler{pool: pool}
}

// ExportDatabase streams a pg_dump to the client as a download.
func (h *DatabaseHandler) ExportDatabase(w http.ResponseWriter, r *http.Request) {
	dbName := r.URL.Query().Get("database")
	if dbName == "" {
		dbName = getHandlerEnv("POSTGRESQL_DATABASE", "postgres")
	}
	format := r.URL.Query().Get("format") // "sql" or "custom"
	if format == "" {
		format = "custom"
	}
	dataOnly := r.URL.Query().Get("dataOnly") == "true"

	var args []string
	var ext, contentType string

	switch format {
	case "sql":
		args = []string{"-h", "/tmp", "-U", getHandlerEnv("POSTGRESQL_USERNAME", "postgres"), "-d", dbName}
		ext = ".sql"
		contentType = "application/sql"
	default:
		args = []string{"-h", "/tmp", "-U", getHandlerEnv("POSTGRESQL_USERNAME", "postgres"), "-d", dbName, "-Fc"}
		ext = ".dump"
		contentType = "application/octet-stream"
	}

	// Data-only: skip schema (CREATE TABLE, etc.), export only INSERT/COPY data
	if dataOnly {
		args = append(args, "--data-only")
		ext = "-data-only" + ext
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s%s", dbName, timestamp, ext)

	cmd := exec.Command("pg_dump", args...)
	cmd.Env = append(os.Environ(),
		"PGPASSWORD="+getHandlerEnv("POSTGRESQL_PASSWORD", ""),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to create pipe: " + err.Error()})
		return
	}

	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "pg_dump failed to start: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, stdout); err != nil {
		log.Printf("[database] export stream error: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		log.Printf("[database] pg_dump error: %v", err)
	}
}

// ImportDatabase accepts a SQL/dump file upload and restores it.
func (h *DatabaseHandler) ImportDatabase(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(512 << 20); err != nil { // 512MB max
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "failed to parse upload: " + err.Error()})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, model.APIResponse{Error: "file required"})
		return
	}
	defer file.Close()

	dbName := r.FormValue("database")
	if dbName == "" {
		dbName = getHandlerEnv("POSTGRESQL_DATABASE", "postgres")
	}

	// Import options
	dataOnly := r.FormValue("dataOnly") == "true"
	disableTriggers := r.FormValue("disableTriggers") == "true"
	singleTx := r.FormValue("singleTransaction") == "true"
	clean := r.FormValue("clean") == "true"
	noTablespaces := r.FormValue("noTablespaces") == "true"

	// Save uploaded file to temp
	tmpFile, err := os.CreateTemp("", "pgaio-import-*"+filepath.Ext(header.Filename))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: "failed to save upload"})
		return
	}
	tmpFile.Close()

	// Determine restore method based on file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	pgUser := getHandlerEnv("POSTGRESQL_USERNAME", "postgres")

	// Build options description for logging
	var opts []string
	if dataOnly {
		opts = append(opts, "data-only")
	}
	if disableTriggers {
		opts = append(opts, "disable-triggers")
	}
	if singleTx {
		opts = append(opts, "single-tx")
	}
	if clean {
		opts = append(opts, "clean")
	}
	if noTablespaces {
		opts = append(opts, "no-tablespaces")
	}

	go func() {
		defer os.Remove(tmpPath)
		optsStr := "none"
		if len(opts) > 0 {
			optsStr = strings.Join(opts, ", ")
		}
		log.Printf("[database] import started [%s]: %s (%s) into %s", optsStr, header.Filename, ext, dbName)

		env := append(os.Environ(), "PGPASSWORD="+getHandlerEnv("POSTGRESQL_PASSWORD", ""))

		// If clean mode: truncate all user tables before import
		if clean {
			log.Println("[database] cleaning: truncating all user tables...")
			truncSQL := `DO $$ DECLARE r RECORD; BEGIN
				FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public') LOOP
					EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' CASCADE';
				END LOOP; END $$;`
			truncCmd := exec.Command("psql", "-h", "/tmp", "-U", pgUser, "-d", dbName, "-c", truncSQL)
			truncCmd.Env = env
			if out, err := truncCmd.CombinedOutput(); err != nil {
				log.Printf("[database] truncate warning: %v\n%s", err, string(out))
			} else {
				log.Println("[database] truncate completed")
			}
		}

		var cmd *exec.Cmd

		if ext == ".dump" || ext == ".backup" {
			// Custom format — use pg_restore
			restoreArgs := []string{"-h", "/tmp", "-U", pgUser, "-d", dbName,
				"--no-owner", "--no-privileges"}
			if dataOnly {
				restoreArgs = append(restoreArgs, "--data-only")
			}
			if disableTriggers {
				restoreArgs = append(restoreArgs, "--disable-triggers")
			}
			if singleTx {
				restoreArgs = append(restoreArgs, "--single-transaction")
			}
			if noTablespaces {
				restoreArgs = append(restoreArgs, "--no-tablespaces")
			}
			restoreArgs = append(restoreArgs, tmpPath)
			cmd = exec.Command("pg_restore", restoreArgs...)
		} else {
			// SQL format — use psql
			psqlArgs := []string{"-h", "/tmp", "-U", pgUser, "-d", dbName}
			if singleTx {
				psqlArgs = append(psqlArgs, "--single-transaction")
			}
			psqlArgs = append(psqlArgs, "-f", tmpPath)
			cmd = exec.Command("psql", psqlArgs...)
		}
		cmd.Env = env

		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[database] import failed: %v\n%s", err, string(out))
		} else {
			log.Printf("[database] ✅ import completed: %s", header.Filename)
		}
	}()

	writeJSON(w, http.StatusAccepted, model.APIResponse{
		Success: true,
		Data: map[string]string{
			"message":  fmt.Sprintf("Import started for %s into %s", header.Filename, dbName),
			"status":   "running",
			"filename": header.Filename,
		},
	})
}

// ListDatabases returns a list of databases.
func (h *DatabaseHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT datname FROM pg_database
		WHERE datistemplate = false
		ORDER BY datname
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			dbs = append(dbs, name)
		}
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: dbs})
}

func getHandlerEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
