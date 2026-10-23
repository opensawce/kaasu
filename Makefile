.PHONY: dev test lint build
dev:
	go run ./cmd/kaasu serve
test:
	go test -race ./...
lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/kaasu ./cmd/kaasu
