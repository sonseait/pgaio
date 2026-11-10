# Build and push the container image

Use Docker Engine with Compose v2 or newer and BuildKit. Run these commands from the repository root. The build uses the existing root Dockerfile and repository context. Only `linux/amd64` is supported by the bundled WAL-G binary.

Copy the example configuration:

```bash
cp build/.env.example build/.env
```

Set these variables in `build/.env`:

- `REGISTRY`: registry hostname, optionally including a port, without a URL scheme or trailing slash.
- `PROJECT`: registry project or namespace, without leading or trailing slashes.
- `IMAGE_TAG`: image version tag.

The resulting image reference is `${REGISTRY}/${PROJECT}/pgaio:${IMAGE_TAG}`. The example uses fictitious registry and project values. `build/.env` is ignored by Git and excluded from the Docker build context. CI can supply these variables directly through its environment.

Authenticate to the destination registry with `docker login` using its hostname and your existing credentials or credential helper. Keep credentials out of Compose files and image build arguments.

Validate, build, and push:

```bash
docker compose --env-file build/.env -f build/compose.yaml config --quiet
docker compose --env-file build/.env -f build/compose.yaml build
docker compose --env-file build/.env -f build/compose.yaml push
```

The build command creates the configured image tag; the push command publishes that same tag. In CI, omit `--env-file build/.env` when providing `REGISTRY`, `PROJECT`, and `IMAGE_TAG` through the environment.

Use the root `docker-compose.yml` for the local development stack. The Compose file under `build/` only configures image building and publishing.
