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
search_path = '"$user", public, extensions'
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

# Auto-create extensions after PostgreSQL starts
# Install into a dedicated 'extensions' schema so DROP SCHEMA public CASCADE won't remove them
# Install into template1 so all future databases inherit them automatically
(
    sleep 30
    until pg_isready -q; do sleep 2; done

    PG_USER="${POSTGRESQL_USERNAME:-postgres}"
    PG_DB="${POSTGRESQL_DATABASE:-postgres}"
    EXTENSIONS="pg_stat_statements pg_idkit pg_repack"
    EXT_SCHEMA="extensions"

    install_extensions() {
        local db="$1"
        # Create dedicated schema for extensions
        PGPASSWORD="${POSTGRESQL_PASSWORD}" psql -U "$PG_USER" -d "$db" -c \
            "CREATE SCHEMA IF NOT EXISTS $EXT_SCHEMA;" 2>/dev/null

        # Install each extension into the dedicated schema
        for ext in $EXTENSIONS; do
            PGPASSWORD="${POSTGRESQL_PASSWORD}" psql -U "$PG_USER" -d "$db" -c \
                "CREATE EXTENSION IF NOT EXISTS $ext SCHEMA $EXT_SCHEMA;" 2>/dev/null && \
                echo "✅ $ext installed in $db.$EXT_SCHEMA" || \
                echo "⚠️  $ext failed in $db"
        done
    }

    install_extensions "template1"
    install_extensions "$PG_DB"

    echo "✅ Extensions ready in '$EXT_SCHEMA' schema (global search_path set in postgresql.conf)"
) &

# Execute original bitnami entrypoint
exec /opt/bitnami/scripts/postgresql/entrypoint.sh "$@"
