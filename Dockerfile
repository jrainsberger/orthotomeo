# Builds cmd/orthotomeo-web - the HTTP API + local web UI - the one
# transport actually shaped like a long-running service. The other
# transports (CLI, MCP, desktop) are invoked locally and don't gain
# anything from a container.
#
# The corpus and the derived DB are NOT baked into this image: the corpus
# is an external, separately-licensed input this repo never ships, and the
# DB is a regenerable build artifact, never checked in (see README "Design
# principles" - read-only, disposable database). Build the DB on the host
# first (see README "Building the database"), then mount it in at
# /data/orthotomeo.db.
#
# modernc.org/sqlite is a pure-Go SQLite driver - no cgo, no C toolchain,
# so the runtime stage can be minimal (distroless static, no shell, no
# package manager - nothing to gain a foothold with, matching the engine's
# own read-only posture at the container level too).

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/orthotomeo-web ./cmd/orthotomeo-web

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/orthotomeo-web /orthotomeo-web
EXPOSE 8420
ENTRYPOINT ["/orthotomeo-web", "--db", "/data/orthotomeo.db"]
