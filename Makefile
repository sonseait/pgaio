.PHONY: build dev tidy vet test update

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
