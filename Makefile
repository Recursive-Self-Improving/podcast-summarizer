.PHONY: help build run test cover fmt vet tidy check clean

APP := podcast-summarizer
CMD := ./cmd/bot
BUILD_DIR := bin

help:
	@printf 'Available targets:\n'
	@printf '  make build  Build the bot binary\n'
	@printf '  make run    Run the bot\n'
	@printf '  make test   Run all tests\n'
	@printf '  make cover  Run tests with coverage\n'
	@printf '  make fmt    Format Go code\n'
	@printf '  make vet    Run go vet\n'
	@printf '  make tidy   Tidy Go modules\n'
	@printf '  make check  Run fmt, vet, and tests\n'
	@printf '  make clean  Remove build artifacts\n'

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(APP) $(CMD)

run:
	go run $(CMD)

test:
	go test ./...

cover:
	go test -cover ./...

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

tidy:
	go mod tidy

check: fmt vet test

clean:
	rm -rf $(BUILD_DIR)
