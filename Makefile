APP_NAME := openclaw-assistant

.PHONY: run dev test build clean

run:
	go run ./cmd/$(APP_NAME)

dev:
	go run ./cmd/$(APP_NAME)

test:
	go test ./...

build:
	go build -o bin/$(APP_NAME) ./cmd/$(APP_NAME)

clean:
	rm -rf bin coverage.out
