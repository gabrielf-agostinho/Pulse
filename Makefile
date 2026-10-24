.PHONY: all fmt vet test race bench build run snapshot release-dry

all: fmt vet test build

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test -count=1 ./...

race:
	CGO_ENABLED=1 go test -race -count=1 ./...

bench:
	go test -run '^$$' -bench . -benchtime 20x ./internal/storage/... ./internal/collectors/... ./internal/tui/...

build:
	CGO_ENABLED=0 go build -trimpath -o pulse ./cmd/pulse

run:
	go run ./cmd/pulse --config configs/config.example.yaml

snapshot:
	go run ./cmd/pulse snapshot --config configs/config.example.yaml --wait 3m --out snapshots

release-dry:
	goreleaser release --snapshot --clean
