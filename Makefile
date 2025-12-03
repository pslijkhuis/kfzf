.PHONY: build install clean test lint

BINARY_NAME=kfzf
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/kfzf

install: build
	install -m 755 $(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME)

install-global: build
	sudo install -m 755 $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)
	rm -f /tmp/kfzf.sock

test:
	go test -v ./...

test-zsh: build
	zsh test/completion_test.zsh

test-all: test test-zsh

lint:
	golangci-lint run

run-server: build
	./$(BINARY_NAME) server -f --log-level=debug

# Generate default config
config-init: build
	./$(BINARY_NAME) config init

# Show ZSH completion script
zsh-completion: build
	./$(BINARY_NAME) zsh-completion
