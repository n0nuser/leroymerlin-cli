.PHONY: build test vet lint fmt check docs docs-check install

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/leroymerlin ./cmd/leroymerlin

test:
	go test ./... -count=1

vet:
	go vet ./...

lint:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

fmt:
	gofmt -w .

check: vet lint test
	test -z "$$(gofmt -l .)"

# docs regenerates the command reference from the command tree; docs-check
# (run in CI) fails when the committed reference no longer matches.
docs:
	go run ./cmd/docsgen

docs-check: docs
	git diff --exit-code -- docs-site/content/docs/reference
	test -z "$$(git ls-files --others --exclude-standard -- docs-site/content/docs/reference)"

install:
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/leroymerlin
