all: lint test

.PHONY: lint
lint:
	go tool -modfile=tools/go.mod golangci-lint run

.PHONY: test
test:
	go test -v ./...
