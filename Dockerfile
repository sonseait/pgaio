# ============================
# PGAIO — All-in-One PostgreSQL
# ============================
# Stage 1: Build Go backend
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY backend/ .
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o pgaio .

# ============================
# Stage 2: Build PgBouncer
FROM debian:bookworm-slim AS pgbouncer-builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential pkg-config libevent-dev libssl-dev curl ca-certificates && \
    curl -sSL https://www.pgbouncer.org/downloads/files/1.24.0/pgbouncer-1.24.0.tar.gz | tar xz && \
    cd pgbouncer-1.24.0 && \
    ./configure --prefix=/usr/local && \
    make -j$(nproc) && make install && \
    strip /usr/local/bin/pgbouncer

# ============================
# Stage 3: Final image
FROM bitnami/postgresql:latest

USER root

# Ensure UID 1001 has a passwd entry (required by wal-g/Go os/user)
RUN echo "pgaio:x:1001:0:PGAIO User:/home/pgaio:/bin/bash" >> /etc/passwd && \
    mkdir -p /home/pgaio && chown 1001:0 /home/pgaio

# Install runtime deps only (libevent + openssl for pgbouncer)
RUN install_packages curl libevent openssl

# Install WAL-G (pre-downloaded to avoid Docker network issues)
COPY bin/wal-g-pg-24.04-amd64-v3.0.8.tar.gz /tmp/walg.tar.gz
RUN tar -xzf /tmp/walg.tar.gz -C /usr/local/bin && \
    mv /usr/local/bin/wal-g-pg-24.04-amd64 /usr/local/bin/wal-g && \
    rm /tmp/walg.tar.gz

# Install pg_idkit extension (pre-downloaded)
COPY bin/pg_idkit-0.4.0-pg18-gnu.tar.gz /tmp/pg_idkit.tar.gz
RUN tar -xf /tmp/pg_idkit.tar.gz -C /tmp && \
    mkdir -p /opt/bitnami/postgresql/lib /opt/bitnami/postgresql/share/extension && \
    cp /tmp/pg_idkit-0.4.0/lib/postgresql/pg_idkit.so /opt/bitnami/postgresql/lib/ && \
    cp /tmp/pg_idkit-0.4.0/share/postgresql/extension/pg_idkit* /opt/bitnami/postgresql/share/extension/ && \
    rm -rf /tmp/pg_idkit*

# Install pg_repack extension + CLI (pre-built for PG18)
COPY bin/pg_repack-1.5.3-pg18-gnu.tar.gz /tmp/pg_repack.tar.gz
RUN tar -xf /tmp/pg_repack.tar.gz -C /tmp && \
    cp /tmp/lib/pg_repack.so /opt/bitnami/postgresql/lib/ && \
    cp /tmp/extension/pg_repack* /opt/bitnami/postgresql/share/extension/ && \
    cp /tmp/bin/pg_repack /usr/local/bin/ && \
    chmod +x /usr/local/bin/pg_repack && \
    rm -rf /tmp/pg_repack* /tmp/lib /tmp/bin /tmp/extension

# Copy PgBouncer binary from builder
COPY --from=pgbouncer-builder /usr/local/bin/pgbouncer /usr/local/bin/pgbouncer
RUN chmod +x /usr/local/bin/pgbouncer

# Copy Go backend binary
COPY --from=builder /build/pgaio /usr/local/bin/pgaio
RUN chmod +x /usr/local/bin/pgaio

# PgBouncer config
RUN mkdir -p /etc/pgbouncer /var/log/pgbouncer /var/run/pgbouncer
COPY pgbouncer/pgbouncer.ini /etc/pgbouncer/pgbouncer.ini
RUN touch /etc/pgbouncer/userlist.txt && \
    chown -R 1001:0 /etc/pgbouncer /var/log/pgbouncer /var/run/pgbouncer

# PostgreSQL entrypoint
COPY postgresql/docker-entrypoint-pg.sh /docker-entrypoint-pg.sh
RUN chmod +x /docker-entrypoint-pg.sh

# Master entrypoint
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Fix /tmp permissions (tar extraction in RUN commands can change /tmp perms)
RUN chmod 1777 /tmp

# Expose ports: PG=5432, PgBouncer=6432, Web=8080
EXPOSE 5432 6432 8080

USER 1001

ENTRYPOINT ["/docker-entrypoint.sh"]
