.PHONY: all build test clean templ run-gateway run-worker run-janitor up down

all: templ build test

templ:
	templ generate

test:
	go test -v -count=1 -race ./...

build: templ
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/worker ./cmd/worker
	go build -o bin/janitor ./cmd/janitor

run-gateway: templ
	go run ./cmd/gateway

run-worker:
	go run ./cmd/worker

run-janitor:
	go run ./cmd/janitor

up:
	docker compose up --build

down:
	docker compose down

clean:
	rm -rf bin/ tmp/ uploads/ storage/
