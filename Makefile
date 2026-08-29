.PHONY: build test run tidy release

VERSION ?= 1.0.0
LDFLAGS := -s -w -X github.com/rursache/always-green/internal/cli.version=$(VERSION)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o always-green ./cmd/always-green

release:
	./build.sh

test:
	go test ./...

run: build
	./always-green

tidy:
	go mod tidy
