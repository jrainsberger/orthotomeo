# Standard targets for orthotomeo. Run from the repo root.
#
# CORPUS/REFERENCE are the two external roots cmd/build reads from - see
# README "Building the database" and "Reading attestation codes" for what
# goes under each. They have no default: the corpus is never checked in.
#
#   make build-db CORPUS=/path/to/bible-text-root REFERENCE=/path/to/reference-root

DB       ?= data/orthotomeo.db
CORPUS   ?=
REFERENCE ?=

.PHONY: all build build-cli build-mcp build-web build-desktop \
        build-db test vet fmt fmt-check lint clean \
        docker-build docker-run

all: build

## Build all four transport binaries.
build: build-cli build-mcp build-web build-desktop

build-cli:
	go build -o orthotomeo ./cmd/orthotomeo

build-mcp:
	go build -o orthotomeo-mcp ./cmd/orthotomeo-mcp

build-web:
	go build -o orthotomeo-web ./cmd/orthotomeo-web

## -H=windowsgui suppresses the console window; harmless (ignored) on
## non-Windows GOOS.
build-desktop:
	go build -ldflags -H=windowsgui -o orthotomeo-desktop ./cmd/orthotomeo-desktop

## Build the derived SQLite DB from the corpus. Fails loudly if CORPUS/
## REFERENCE aren't given - cmd/build itself refuses to guess a path.
build-db:
	@if [ -z "$(CORPUS)" ] || [ -z "$(REFERENCE)" ]; then \
		echo "usage: make build-db CORPUS=<path> REFERENCE=<path> [DB=$(DB)]"; \
		exit 1; \
	fi
	go run ./cmd/build --corpus "$(CORPUS)" --reference "$(REFERENCE)" --out "$(DB)" --verify

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

## Non-mutating check for CI - fails if anything isn't already gofmt'd.
fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

## The same three checks run throughout this project's own sessions,
## in one target.
lint: fmt-check vet test

clean:
	rm -f orthotomeo orthotomeo.exe \
	      orthotomeo-mcp orthotomeo-mcp.exe \
	      orthotomeo-web orthotomeo-web.exe \
	      orthotomeo-desktop orthotomeo-desktop.exe

docker-build:
	docker build -t orthotomeo-web .

## Mounts $(DB) read-only into the container at the path the image expects.
docker-run:
	docker run --rm -p 8420:8420 -v "$(abspath $(DB))":/data/orthotomeo.db:ro orthotomeo-web

## Cloud Run build/deploy targets live in deploy/cloud.mk - deliberately
## kept out of the public repo (see .gitignore), so this include is silent
## and this Makefile stays clean of any cloud-deploy reference for anyone
## cloning the public repo. `-include` (not `include`): don't error if the
## file doesn't exist.
-include deploy/cloud.mk
