# syntax=docker/dockerfile:1

# ---- web frontend build -----------------------------------------
# Builds the SvelteKit admin UI to static files (adapter-static, see
# web/vite.config.ts) -- internal/web.Server serves these straight off
# disk (spaFileServer), no embedding needed.
FROM node:24-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Go build ------------------------------------------------------
# CGO_ENABLED=0 throughout: every dependency here, sqlite included
# (modernc.org/sqlite), is pure Go by design (see CLAUDE.md) so cross-
# compiling for both target arches needs no C toolchain at all.
FROM golang:1.27-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/bbs ./cmd/bbs && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/mailer ./cmd/mailer && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/web ./cmd/web

# ---- runtime ---------------------------------------------------------
# debian:trixie-slim, not alpine/distroless: DOS door support (see
# docs/adding-a-door.md) needs a real dosbox-x package with its own
# shared-library dependencies (SDL2 et al.), which apt resolves
# cleanly here and would otherwise have to be hand-assembled on musl.
# trixie specifically because dosbox-x isn't packaged for bookworm
# (Debian 12) at all -- confirmed live, "E: Unable to locate package
# dosbox-x" there.
FROM debian:trixie-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        dosbox-x \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=go-build /out/bbs /out/mailer /out/web ./bin/
COPY --from=web-build /src/web/build ./web/build
COPY docker-entrypoint.sh ./

# configs/menus and configs/screens are versioned code but also
# runtime-editable: the web admin's ANSI Designer (see
# internal/web/screens_handler.go) writes .ans files straight back
# into configs/screens, and a sysop can hand-edit configs/menus too --
# both need to survive a container recreate the same way data/ does,
# so docker-compose.yml bind-mounts them as directories. They're
# baked in here only as a reference copy under configs-defaults/, and
# docker-entrypoint.sh seeds the real (bind-mounted) configs/menus and
# configs/screens from it on startup without ever overwriting a file
# that's already there -- see that script's own doc comment for why.
COPY configs/menus ./configs-defaults/menus
COPY configs/screens ./configs-defaults/screens

# configs/bbs.yaml and configs/web.yaml are gitignored (real secrets --
# BinkP uplink passwords, JWT signing key path) and so aren't baked
# into the image; bind-mount them individually (see docker-compose.yml,
# and configs/bbs.yaml.example to start from) or every daemon below
# falls back to its own built-in defaults. data/ is a volume for the
# same reason -- SQLite database, SSH host key, uploaded files, and
# door installs (see docs/adding-a-door.md) all need to survive a
# container recreate.
VOLUME ["/app/data"]

# Doors run under DOSBox-X headlessly (SDL_VIDEODRIVER=dummy) -- see
# docs/adding-a-door.md's "-socket N"/"inhsocket:1" mechanism, which,
# unlike DOSBox-X's plain TCP-based nullmodem modes, was confirmed
# live to need no real or virtual display at all.
ENV SDL_VIDEODRIVER=dummy

EXPOSE 2323 2222 8090 24554

# ENTRYPOINT only runs the seed step above and then execs whatever
# command was given -- cmd/bbs, cmd/mailer, and cmd/web are still
# three independent daemons meant to run as separate containers/
# services sharing this same image (see cmd/mailer/main.go's own doc
# comment, and docker-compose.yml), picked with `command:` per service.
ENTRYPOINT ["./docker-entrypoint.sh"]
CMD ["./bin/bbs"]
