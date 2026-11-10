# PGAIO contributor instructions

## Working setup and checks

- Use the Go version declared in `backend/go.mod`; run Go commands from `backend/`.
- Read `README.md` for configuration, runtime requirements, and backup API usage.
- Run `make dev` with a reachable PostgreSQL instance configured through environment variables.
- Run `make build`, `make test`, and `make vet` for backend changes.
- Format changed Go files with `gofmt`; use the standard `testing` package and adjacent `*_test.go` files.
- Start the local container stack with `docker compose up --build -d`.
- Use `build/compose.yaml` for image builds and pushes; see `docs/container-build.md` for commands and environment setup. Keep Makefile targets limited to Go tooling.
- For backup/recovery changes, run `python3 tests/integration/backup_api.py --image <local-runtime-image>` against its isolated test stack; never target live data.

## Go conventions

- Preserve the existing `net/http`, pgx, and embedded static asset stack.
- Use lowercase package names, descriptive snake_case Go filenames, exported PascalCase names, and unexported camelCase names.
- Keep import groups consistent with neighboring files and let `gofmt` format them.
- Keep handlers focused on decoding, validation, status codes, and existing API response helpers; put database and job logic in services.
- Pass request contexts through database and subprocess operations, check errors, close rows and response bodies, and cancel derived contexts.
- Parameterize SQL values and quote dynamic identifiers with pgx facilities; validate backup names, paths, and restore targets before side effects.
- Preserve authentication for administrative routes and the asynchronous job contract for long operations.
- Protect shared state consistently with existing locks; stop background workers and release owned pools during shutdown.

## Embedded dashboard conventions

- Keep plain JavaScript, HTML, and CSS embedded through `backend/web/embed.go`; no separate frontend build is required.
- Match existing four-space JavaScript indentation, semicolons, camelCase methods, and page-object patterns.
- Reuse existing API, escaping, and styling helpers; do not interpolate untrusted values into HTML.
- Preserve keyboard access, visible focus, form labels, and useful loading and error states.

## Shell, containers, and maintenance

- Preserve Bash entrypoint behavior, quote variable expansions, check failing commands, and retain graceful process shutdown.
- Keep Docker changes compatible with the bundled PostgreSQL runtime and the current linux/amd64 WAL-G binary.
- Keep functions cohesive and names self-explanatory; avoid duplication, unused code, and unnecessary abstractions or dependencies.
- Comment only non-obvious constraints or decisions; preserve legal notices and tooling directives.
- Document changed configuration, APIs, and operational workflows in English under `docs/`, linking from the README when useful.
