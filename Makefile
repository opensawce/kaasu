.PHONY: dev test test-cover lint build fuzz
dev:
	go run ./cmd/kaasu serve
test:
	go test -race ./...
test-cover:
	go test -race -coverprofile=coverage.out ./...
	scripts/coverage-gate.sh
lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...
fuzz:
	scripts/fuzz.sh 20s
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/kaasu ./cmd/kaasu
