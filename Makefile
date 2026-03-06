.PHONY: build dev tidy vet test docker up down logs clean

# ========================
# Go Backend
# ========================

build:
	cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/pgaio .

dev:
	cd backend && go run .

tidy:
	cd backend && go mod tidy

vet:
	cd backend && go vet ./...

test:
	cd backend && go test ./... -v

update:
	cd backend && go get -u all && go mod tidy

# ========================
# Docker
# ========================

docker:
	docker build -t pgaio .

up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f

logs-pgaio:
	docker compose logs -f pgaio

logs-minio:
	docker compose logs -f minio

# ========================
# Utilities
# ========================

clean:
	rm -rf bin/
	docker compose down -v 2>/dev/null || true

ps:
	docker compose ps

shell:
	docker compose exec pgaio bash

psql:
	docker compose exec pgaio psql -U postgres