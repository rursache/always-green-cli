.PHONY: build test run tidy release

VERSION ?= 1.0.0
LDFLAGS := -s -w -X github.com/rursache/always-green-cli/internal/cli.version=$(VERSION)

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o always-green-cli ./cmd/always-green-cli
	ln -sf always-green-cli always-green

release:
	./build.sh

test:
	go test ./...

run: build
	./always-green-cli

tidy:
	go mod tidy
