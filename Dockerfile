# syntax=docker/dockerfile:1.19

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
# Stage 3: Re-layer the flattened PostgreSQL base without changing its files
# Pinned multi-architecture digest; do not replace with a mutable tag.
FROM bitnami/postgresql@sha256:7b9c5292a8e8b38e7172705cc6e49e34b9a8d9c297c4c7ae04130a3e6a28f3b0 AS postgresql-base

FROM scratch AS runtime-base

# The upstream image is a single ~400 MB uncompressed layer. Copy disjoint
# directories so registries can transfer and cache smaller layers separately.
# Each excluded directory is restored by a subsequent COPY.
COPY --from=postgresql-base --exclude=usr --exclude=opt / /
COPY --from=postgresql-base --exclude=lib /usr/ /usr/
COPY --from=postgresql-base /usr/lib/ /usr/lib/
COPY --from=postgresql-base --exclude=bitnami/postgresql /opt/ /opt/
COPY --from=postgresql-base --exclude=lib --exclude=share /opt/bitnami/postgresql/ /opt/bitnami/postgresql/
COPY --from=postgresql-base /opt/bitnami/postgresql/lib/ /opt/bitnami/postgresql/lib/
COPY --from=postgresql-base /opt/bitnami/postgresql/share/ /opt/bitnami/postgresql/share/

# FROM scratch does not inherit the upstream runtime configuration.
ARG TARGETARCH
ENV PATH="/opt/bitnami/common/bin:/opt/bitnami/postgresql/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    HOME="/" \
    OS_ARCH="${TARGETARCH}" \
    OS_FLAVOUR="photon-5" \
    OS_NAME="linux" \
    APP_VERSION="18.6.0" \
    BITNAMI_APP_NAME="postgresql" \
    IMAGE_REVISION="5" \
    LANG="en_US.UTF-8" \
    LANGUAGE="en_US:en" \
    NSS_WRAPPER_LIB="/opt/bitnami/common/lib/libnss_wrapper.so" \
    OPENSSL_FIPS="yes" \
    OPENSSL_FIPS_PROVIDER_PATH="/etc/ssl/provider_fips.cnf"
SHELL ["/bin/bash", "-o", "errexit", "-o", "nounset", "-o", "pipefail", "-c"]
WORKDIR /
VOLUME ["/bitnami/postgresql", "/docker-entrypoint-initdb.d", "/docker-entrypoint-preinitdb.d"]
LABEL org.opencontainers.image.base.name="docker.io/bitnami/postgresql@sha256:7b9c5292a8e8b38e7172705cc6e49e34b9a8d9c297c4c7ae04130a3e6a28f3b0" \
      org.opencontainers.image.version="18.6.0"

# ============================
# Stage 4: Final image
FROM runtime-base AS runtime

USER root

# Ensure UID 1001 has a passwd entry (required by wal-g/Go os/user)
RUN echo "pgaio:x:1001:0:PGAIO User:/home/pgaio:/bin/bash" >> /etc/passwd && \
    mkdir -p /home/pgaio && chown 1001:0 /home/pgaio

# Photon 5's package mirror fails TLS negotiation under linux/amd64. curl and
# OpenSSL are already in the Bitnami image; libevent comes from the Debian build stage.

# Mount pre-downloaded archives so they are never stored in runtime layers.
RUN --mount=type=bind,source=bin/wal-g-pg-24.04-amd64-v3.0.8.tar.gz,target=/tmp/walg.tar.gz \
    tar -xzf /tmp/walg.tar.gz -C /usr/local/bin && \
    mv /usr/local/bin/wal-g-pg-24.04-amd64 /usr/local/bin/wal-g

# Install pg_idkit extension (pre-downloaded)
RUN --mount=type=bind,source=bin/pg_idkit-0.4.0-pg18-gnu.tar.gz,target=/tmp/pg_idkit.tar.gz \
    tar -xf /tmp/pg_idkit.tar.gz -C /tmp && \
    mkdir -p /opt/bitnami/postgresql/lib /opt/bitnami/postgresql/share/extension && \
    cp /tmp/pg_idkit-0.4.0/lib/postgresql/pg_idkit.so /opt/bitnami/postgresql/lib/ && \
    cp /tmp/pg_idkit-0.4.0/share/postgresql/extension/pg_idkit* /opt/bitnami/postgresql/share/extension/ && \
    rm -rf /tmp/pg_idkit-0.4.0

# Install pg_repack extension + CLI (pre-built for PG18)
RUN --mount=type=bind,source=bin/pg_repack-1.5.3-pg18-gnu.tar.gz,target=/tmp/pg_repack.tar.gz \
    tar -xf /tmp/pg_repack.tar.gz -C /tmp && \
    cp /tmp/lib/pg_repack.so /opt/bitnami/postgresql/lib/ && \
    cp /tmp/extension/pg_repack* /opt/bitnami/postgresql/share/extension/ && \
    cp /tmp/bin/pg_repack /usr/local/bin/ && \
    chmod +x /usr/local/bin/pg_repack && \
    rm -rf /tmp/lib /tmp/bin /tmp/extension && \
    printf '%s\n' /opt/bitnami/postgresql/lib > /etc/ld.so.conf.d/postgresql.conf && \
    ldconfig

# Copy PgBouncer binary from builder
COPY --from=pgbouncer-builder /usr/local/bin/pgbouncer /usr/local/bin/pgbouncer
COPY --from=pgbouncer-builder /lib/*/libevent-2.1.so.7 /usr/lib/
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
