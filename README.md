# PGAIO

**PGAIO** is an all-in-one PostgreSQL management and observability workspace. It packages PostgreSQL, a Go web dashboard, PgBouncer, WAL-G, and useful PostgreSQL extensions into one container so a database can be monitored, maintained, backed up, and tuned from a single interface.

> **Project status:** Early-stage software. Review the security, storage, backup, and network settings before using it with production data.

## What it includes

- Real-time PostgreSQL monitoring for database activity, connections, replication, WAL, system resources, and background writer metrics.
- A browser-based SQL editor with schema browsing, snippets, query history, slow-query inspection, and saved execution plans.
- Backup and recovery workflows powered by WAL-G and S3-compatible object storage, including scheduled backups, verification, retention, and point-in-time recovery.
- PgBouncer metrics and administrative controls, plus connection profiles for routing supported features to PostgreSQL or PgBouncer.
- Maintenance tooling for vacuum and bloat analysis, lock trees, index recommendations, online `pg_repack`, extensions, roles, configuration, logs, and database import/export jobs.
- A tuning wizard, schema-drift checks, Telegram health alerts, and TOTP protection for state-changing actions.

## Architecture

```text
Browser
  |
  v
PGAIO web UI and Go API (:8080)
  |                |
  v                v
PostgreSQL (:5432) PgBouncer (:6432)
  |
  v
WAL-G --> S3-compatible storage
```

The development Compose stack also starts MinIO for local S3-compatible backup testing and Redis for experimentation.

## Quick start

### Prerequisites

- Docker Engine with Docker Compose v2
- At least 2 GB of available memory for the local stack

Start the development environment:

```bash
docker compose up --build -d
```

Open the dashboard at [http://localhost:8080](http://localhost:8080). On first use, set up TOTP authentication in the UI with an authenticator application.

Local service endpoints:

| Service | Address | Development credentials |
| --- | --- | --- |
| PGAIO dashboard | `http://localhost:8080` | TOTP setup required on first use |
| PostgreSQL | `localhost:5432` | user `postgres`, password `postgres` |
| MinIO API | `http://localhost:9000` | `minioadmin` / `minioadmin` |
| MinIO Console | `http://localhost:9001` | `minioadmin` / `minioadmin` |
| Redis | `localhost:6379` | no password |

Follow the logs while the services start:

```bash
docker compose logs -f pgaio
```

Stop the stack:

```bash
docker compose down
```

## Configuration

The bundled `docker-compose.yml` is intended for development and testing. It configures MinIO and WAL-G with public sample credentials and an example encryption key. Do not deploy those values or expose the listed ports in production.

For a real S3-compatible backup target, provide these variables to the `pgaio` service:

| Variable | Purpose |
| --- | --- |
| `WALG_S3_PREFIX` | Backup prefix, for example `s3://my-bucket/wal-g` |
| `AWS_ACCESS_KEY_ID` | Object-storage access key |
| `AWS_SECRET_ACCESS_KEY` | Object-storage secret key |
| `AWS_ENDPOINT` | S3-compatible endpoint; omit or adapt for AWS S3 |
| `AWS_REGION` | Storage region, default `us-east-1` |
| `AWS_S3_FORCE_PATH_STYLE` | Use path-style S3 URLs when required by the provider |
| `WALG_LIBSODIUM_KEY` | Optional WAL-G encryption key; generate and store it securely |

Useful application variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `PGAIO_PORT` | `8080` | Port for the web UI and API |
| `DATABASE_URL` | - | PostgreSQL connection string for the Go backend |
| `PGHOST` / `PGPORT` | `/tmp` / `5432` | PostgreSQL host and port when `DATABASE_URL` is not set |
| `POSTGRESQL_USERNAME` / `POSTGRESQL_PASSWORD` | `postgres` / `postgres` | Primary PostgreSQL and PgBouncer admin credentials |
| `POSTGRESQL_DATABASE` | `postgres` | Default database |
| `PGBOUNCER_ADMIN_ADDR` | `127.0.0.1:6432` | PgBouncer admin endpoint |
| `PGAIO_TOTP_SECRET` | - | Preconfigure the TOTP secret instead of completing first-run setup |

PGAIO persists its dashboard settings and TOTP secret under `/bitnami/postgresql`. Mount persistent volumes for PostgreSQL data and this directory before relying on backups, scheduled jobs, settings, or authentication across container recreation.

## Development

The backend requires Go 1.26 or newer. The frontend is embedded in the Go binary, so no Node.js build step is needed.

```bash
make build       # Build the Go backend to bin/pgaio
make dev         # Run the backend locally
make test        # Run Go tests
make vet         # Run Go vet
make up          # Build and start the Docker Compose stack
```

The main implementation lives in `backend/`:

```text
backend/
  handler/       HTTP handlers for the dashboard API
  service/       PostgreSQL, WAL-G, S3, monitoring, and job services
  server/        Route registration and HTTP server setup
  web/           Embedded static dashboard assets
pgbouncer/       PgBouncer configuration
postgresql/      PostgreSQL startup and extension setup
```

## Security notes

- Treat the dashboard as an administrative interface. Place it behind a trusted network boundary and HTTPS reverse proxy; do not publish it directly to the internet.
- Configure TOTP before allowing other users access. Sessions are held in memory and expire after 15 minutes of inactivity.
- Use dedicated least-privilege credentials where possible. Several features intentionally require elevated PostgreSQL permissions, including maintenance, extension management, configuration changes, and database restore.
- Keep object-storage credentials and `WALG_LIBSODIUM_KEY` outside version control, ideally in a secrets manager.
- Test backups and restore procedures regularly. A successful upload is not a substitute for a verified restore.

## Built with

- [PostgreSQL](https://www.postgresql.org/)
- [PgBouncer](https://www.pgbouncer.org/)
- [WAL-G](https://wal-g.readthedocs.io/)
- [Go](https://go.dev/) and [pgx](https://github.com/jackc/pgx)
- [MinIO](https://min.io/) for the local S3-compatible development environment
- `pg_stat_statements`, `pg_idkit`, and `pg_repack`

## Contributing

Issues and pull requests are welcome. For a code change, please keep the scope focused and run:

```bash
make vet
make test
```

If you are reporting a security issue, please do not open a public issue with credentials, backups, or a proof of exploit.

## License

This repository does not currently include a license file. Add an explicit open-source license (for example, Apache-2.0 or MIT) before publishing or accepting external contributions, then update this section to link to it.
