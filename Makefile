.PHONY: build run clean test dev install bench hash-password fmt lint

BINARY=nginxplorer
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/nginxplorer

run: build
	./$(BINARY)

dev:
	go run ./cmd/nginxplorer --config configs/nginxplorer.example.yaml

test:
	go test -v -race ./...

bench:
	go test -bench=. -benchmem ./internal/metrics/...

clean:
	rm -f $(BINARY)

install: build
	sudo install -Dm755 $(BINARY) /usr/local/bin/$(BINARY)
	sudo install -Dm644 configs/nginxplorer.example.yaml /etc/nginxplorer/config.yaml.example
	sudo install -Dm644 configs/nginx/nginxplorer-log.conf /etc/nginx/conf.d/nginxplorer-log.conf.example
	sudo install -Dm644 configs/nginx/nginxplorer-status.conf /etc/nginx/conf.d/nginxplorer-status.conf.example
	sudo install -Dm644 configs/nginxplorer.service /etc/systemd/system/nginxplorer.service
	@echo "Installed $(BINARY) to /usr/local/bin/"
	@echo "Copy and edit /etc/nginxplorer/config.yaml.example to /etc/nginxplorer/config.yaml"

hash-password:
	@go run ./cmd/nginxplorer hash-password

fmt:
	gofmt -s -w .

lint:
	golangci-lint run ./...
