.PHONY: all build test clean run

BINARY_NAME=bin/linux-agent

all: test build

build:
	@mkdir -p bin
	go build -trimpath -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/linux-agent

test:
	go test -v ./...

clean:
	rm -rf bin/

run: build
	./$(BINARY_NAME)
