.PHONY: build test run tidy release

build:
	go build -trimpath -ldflags="-s -w" -o always-green ./cmd/always-green

release:
	./build.sh

test:
	go test ./...

run: build
	./always-green

tidy:
	go mod tidy
