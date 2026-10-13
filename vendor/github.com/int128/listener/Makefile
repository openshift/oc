.PHONY: test
test:
	go test -v -race ./...

.PHONY: lint
lint:
	go tool -modfile=tools/go.mod golangci-lint run
