.PHONY: build test lint
build:
	go build -o fan ./cmd/fan
test:
	go test ./...
lint:
	golangci-lint run
