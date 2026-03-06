#!/bin/bash
set -e

# ========================
# PostgreSQL Entrypoint
# Sets up WAL-G archiving and backup schedule
# ========================

# Configure WAL archiving for WAL-G
if [ -n "$WALG_S3_PREFIX" ]; then
    echo "📦 Configuring WAL-G archiving..."

    # Set environment for wal-g
    export PGHOST="${PGHOST:-/tmp}"
    export PGUSER="${POSTGRESQL_USERNAME:-postgres}"
    export PGPASSWORD="${POSTGRESQL_PASSWORD}"
    export PGDATABASE="${POSTGRESQL_DATABASE:-postgres}"

    # Backup loop if interval is configured
    if [ -n "$BACKUP_INTERVAL_HOURS" ]; then
        INTERVAL_SECONDS=$((${BACKUP_INTERVAL_HOURS:-6} * 3600))
        echo "📦 Setting up WAL-G backup every ${BACKUP_INTERVAL_HOURS} hours"

        # Create backup script
        cat > /tmp/backup.sh << 'BEOF'
#!/bin/bash
set -e

export PGHOST=${PGHOST:-/tmp}
export PGUSER=${POSTGRESQL_USERNAME:-postgres}
export PGPASSWORD=${POSTGRESQL_PASSWORD}
export PGDATABASE=${POSTGRESQL_DATABASE:-postgres}

echo "[$(date)] Starting incremental backup..."

# Push delta backup (fallback to full if no base exists)
wal-g backup-push /bitnami/postgresql/data --delta-from-name LATEST 2>/dev/null || \
    wal-g backup-push /bitnami/postgresql/data

# Retain only specified number of backups
RETAIN_COUNT=${BACKUP_RETAIN_COUNT:-7}
wal-g delete retain FULL $RETAIN_COUNT --confirm

echo "[$(date)] Backup completed successfully"
BEOF

        chmod +x /tmp/backup.sh

        (
            # Wait for PostgreSQL to be ready
            sleep 60
            while true; do
                /tmp/backup.sh >> /tmp/backup.log 2>&1 || echo "[$(date)] Backup failed" >> /tmp/backup.log
                sleep $INTERVAL_SECONDS
            done
        ) &

        echo "✅ WAL-G backup loop started (interval: ${BACKUP_INTERVAL_HOURS}h)"
    else
        echo "ℹ️  Backup schedule not configured (set BACKUP_INTERVAL_HOURS)"
    fi
else
    echo "ℹ️  WAL-G not configured (set WALG_S3_PREFIX)"
fi

# Configure PostgreSQL file logging for log streaming
PG_LOG_DIR="/opt/bitnami/postgresql/logs"
mkdir -p "$PG_LOG_DIR"
chown 1001:0 "$PG_LOG_DIR" 2>/dev/null || true

# Remove bitnami's default symlinks to /dev/stdout
rm -f "$PG_LOG_DIR/postgresql.log" "$PG_LOG_DIR/postgresql.csv" "$PG_LOG_DIR/postgresql.json" 2>/dev/null || true

# Write override conf for logging
cat > /opt/bitnami/postgresql/conf/conf.d/logging.conf << 'LEOF'
logging_collector = on
log_directory = '/opt/bitnami/postgresql/logs'
log_filename = 'postgresql.log'
log_truncate_on_rotation = off
log_rotation_age = 1d
log_rotation_size = 100MB
log_min_messages = info
log_line_prefix = '%t [%p] %q%u@%d '
LEOF
echo "✅ PostgreSQL file logging enabled ($PG_LOG_DIR)"

# Execute original bitnami entrypoint
exec /opt/bitnami/scripts/postgresql/entrypoint.sh "$@"
