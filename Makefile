BINARY := bin/telegram-ai-agent

.PHONY: build run migrate test race vet fmt check docker codemap clean

## build: compile the bot into bin/
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/bot

## run: start the bot using settings from .env
run:
	go run ./cmd/bot

## migrate: apply database migrations using DB_POSTGRES_URL from .env
migrate:
	go run ./cmd/bot migrate

## test: run all tests (storage tests need TEST_DB_POSTGRES_URL, otherwise they skip)
test:
	go test -p 1 ./...

## race: run all tests with the race detector
race:
	go test -race -p 1 ./...

## vet: report suspicious code
vet:
	go vet ./...

## fmt: format all Go files
fmt:
	gofmt -w .

## check: everything CI should run
check: vet race
	@test -z "$$(gofmt -l .)" || (echo "these files need gofmt:"; gofmt -l .; exit 1)

## docker: build the container image
docker:
	docker build -t telegram-ai-agent .

## codemap: build graphify-out/explorer.html, the code's map, from graphify's graph
codemap:
	python3 docs/codemap/build.py

clean:
	rm -rf bin
