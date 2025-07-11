.PHONY: test
test:
	go test -v -race ./...

.PHONY: lint
lint:
	$(MAKE) -C tools
	./tools/bin/golangci-lint run
