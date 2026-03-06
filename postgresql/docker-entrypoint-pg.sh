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

    echo "✅ WAL-G configured (backup schedule managed via Web UI)"
else
    echo "ℹ️  WAL-G not configured (set WALG_S3_PREFIX)"
fi

# Configure PostgreSQL file logging for log streaming
PG_LOG_DIR="/opt/bitnami/postgresql/logs"
mkdir -p "$PG_LOG_DIR"
chown 1001:0 "$PG_LOG_DIR" 2>/dev/null || true

# Remove bitnami's default symlinks to /dev/stdout
rm -f "$PG_LOG_DIR/postgresql.log" "$PG_LOG_DIR/postgresql.csv" "$PG_LOG_DIR/postgresql.json" 2>/dev/null || true

# Write override conf for logging + extensions
cat > /opt/bitnami/postgresql/conf/conf.d/logging.conf << 'LEOF'
logging_collector = on
log_directory = '/opt/bitnami/postgresql/logs'
log_filename = 'postgresql.log'
log_truncate_on_rotation = off
log_rotation_age = 1d
log_rotation_size = 100MB
log_min_messages = info
log_line_prefix = '%t [%p] %q%u@%d '
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
LEOF

    # Enable WAL archiving for PITR (only if S3 is configured)
    if [ -n "$WALG_S3_PREFIX" ]; then
        cat > /opt/bitnami/postgresql/conf/conf.d/archiving.conf << 'AEOF'
archive_mode = on
archive_command = 'wal-g wal-push %p'
archive_timeout = 300
wal_level = replica
AEOF
        echo "✅ WAL archiving enabled (PITR support active)"
    fi
echo "✅ PostgreSQL file logging + pg_stat_statements enabled ($PG_LOG_DIR)"

# Auto-create pg_stat_statements extension after PostgreSQL starts
(
    sleep 30
    until pg_isready -q; do sleep 2; done
    PGPASSWORD="${POSTGRESQL_PASSWORD}" psql -U "${POSTGRESQL_USERNAME:-postgres}" -d "${POSTGRESQL_DATABASE:-postgres}" -c "CREATE EXTENSION IF NOT EXISTS pg_stat_statements;" 2>/dev/null && \
        echo "✅ pg_stat_statements extension created" || \
        echo "⚠️  pg_stat_statements extension creation failed"
) &

# Execute original bitnami entrypoint
exec /opt/bitnami/scripts/postgresql/entrypoint.sh "$@"
