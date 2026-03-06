#!/bin/bash
set -e

echo "🚀 PGAIO — All-in-One PostgreSQL starting..."

# ========================
# Generate PgBouncer userlist
# ========================
PG_USER="${POSTGRESQL_USERNAME:-postgres}"
PG_PASS="${POSTGRESQL_PASSWORD:-postgres}"

echo "\"${PG_USER}\" \"${PG_PASS}\"" > /etc/pgbouncer/userlist.txt
echo "\"pgbouncer\" \"${PG_PASS}\"" >> /etc/pgbouncer/userlist.txt
echo "✅ PgBouncer userlist generated"

# ========================
# Export env for WAL-G
# ========================
if [ -n "$WALG_S3_PREFIX" ]; then
    export AWS_S3_FORCE_PATH_STYLE="${AWS_S3_FORCE_PATH_STYLE:-true}"
    export WALG_COMPRESSION_METHOD="${WALG_COMPRESSION_METHOD:-lz4}"
    export WALG_DELTA_MAX_STEPS="${WALG_DELTA_MAX_STEPS:-7}"
    export WALG_UPLOAD_CONCURRENCY="${WALG_UPLOAD_CONCURRENCY:-2}"
    export WALG_DOWNLOAD_CONCURRENCY="${WALG_DOWNLOAD_CONCURRENCY:-4}"
    echo "✅ WAL-G configured (prefix: $WALG_S3_PREFIX)"
fi

# ========================
# Export env for PGAIO backend
# ========================
export PGHOST="${PGHOST:-/tmp}"
export PGPORT="${PGPORT:-5432}"
export PGAIO_PORT="${PGAIO_PORT:-8080}"
export PGBOUNCER_ADMIN_ADDR="${PGBOUNCER_ADMIN_ADDR:-127.0.0.1:6432}"
export PGBOUNCER_ADMIN_USER="${PGBOUNCER_ADMIN_USER:-pgbouncer}"

# ========================
# Trap for graceful shutdown
# ========================
cleanup() {
    echo "🛑 Shutting down..."
    kill $PGAIO_PID $PGB_PID 2>/dev/null || true
    wait $PGAIO_PID $PGB_PID 2>/dev/null || true
    echo "👋 All processes stopped"
    exit 0
}
trap cleanup SIGTERM SIGINT

# ========================
# Start PostgreSQL (foreground via entrypoint)
# ========================
echo "🐘 Starting PostgreSQL..."
/docker-entrypoint-pg.sh /opt/bitnami/scripts/postgresql/run.sh &
PG_PID=$!

# Wait for PostgreSQL to be ready
echo "⏳ Waiting for PostgreSQL..."
for i in $(seq 1 60); do
    if pg_isready -h /tmp -U "${PG_USER}" -q 2>/dev/null; then
        echo "✅ PostgreSQL is ready"
        break
    fi
    if [ $i -eq 60 ]; then
        echo "❌ PostgreSQL failed to start"
        exit 1
    fi
    sleep 1
done

# ========================
# Start PgBouncer
# ========================
echo "⚡ Starting PgBouncer..."
pgbouncer /etc/pgbouncer/pgbouncer.ini &
PGB_PID=$!
echo "✅ PgBouncer started (pid: $PGB_PID)"

# ========================
# Start PGAIO Backend
# ========================
echo "🌐 Starting PGAIO Web UI..."
/usr/local/bin/pgaio &
PGAIO_PID=$!
echo "✅ PGAIO Backend started (pid: $PGAIO_PID)"

echo ""
echo "========================================="
echo "  PGAIO — All-in-One PostgreSQL"
echo "  PostgreSQL : :5432"
echo "  PgBouncer  : :6432"
echo "  Web UI     : :${PGAIO_PORT:-8080}"
echo "========================================="
echo ""

# Wait for any process to exit
wait -n $PG_PID $PGB_PID $PGAIO_PID 2>/dev/null

# If any process dies, shut down everything
echo "⚠️  A process exited, shutting down..."
cleanup
