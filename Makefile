BINARY = repo-lens
INSTALL_PATH = $(shell pwd)/$(BINARY)

.PHONY: build install test clean

build:
	go build -o $(BINARY) .

install: build
	@echo "Binary at $(INSTALL_PATH) — already registered in ../.mcp.json"

test:
	go test ./...

clean:
	rm -f $(BINARY)
