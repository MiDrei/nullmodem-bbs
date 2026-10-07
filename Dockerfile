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
# The build shown beside the version (internal/version): pass the
# commit with --build-arg COMMIT=$(git rev-parse --short HEAD); the
# time is when this runs.
ARG COMMIT=""
RUN --mount=type=cache,target=/root/.cache/go-build \
    LDFLAGS="-s -w -X github.com/midrei/nullmodem-bbs/internal/version.Commit=${COMMIT} -X github.com/midrei/nullmodem-bbs/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="$LDFLAGS" -o /out/bbs ./cmd/bbs && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="$LDFLAGS" -o /out/mailer ./cmd/mailer && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="$LDFLAGS" -o /out/web ./cmd/web

# ---- sexyz (Zmodem) ----------------------------------------------------
# Telnet/SSH file and QWK transfers shell out to Synchronet's sexyz
# (see the kit's zmodem package and docs/building-sexyz.md); no distro
# packages it, so it is built from source here. Pinned to the commit
# the Zmodem tests are run against, and built on the same Debian as
# the runtime image so its libc matches -- sexyz needs nothing else.
#
# sexyz is GPL (v2 or later; parts LGPL 2.1 and BSD): the image carries
# its license texts, notices and the exact source it was built from in
# /usr/local/share/doc/sexyz -- see third_party/sexyz/NOTICE and
# docs/third-party.md.
FROM debian:trixie-slim AS sexyz-build
RUN apt-get update && \
    apt-get install -y --no-install-recommends build-essential git ca-certificates && \
    rm -rf /var/lib/apt/lists/*
ARG SBBS_COMMIT=7cf7f2fc56d8383aeb6cd35639977f3815afe7ce
WORKDIR /src
# Just what sexyz is built from, at that one commit: the complete
# corresponding source, and nothing of the (large) rest.
RUN git init -q sbbs && cd sbbs && \
    git remote add origin https://gitlab.synchro.net/main/sbbs.git && \
    git sparse-checkout set --no-cone /src/build/ /src/sbbs3/ /src/xpdev/ /src/hash/ \
        /src/smblib/ /src/encode/ /docs/gpl.txt /docs/lgpl.txt && \
    git fetch -q --depth 1 --filter=blob:none origin "$SBBS_COMMIT" && \
    git checkout -q FETCH_HEAD
# One local fix on top (see the patch's header): sexyz could exit
# before its output thread had sent the session's last bytes. Applied
# before the source archive is made, so the archive is what was built.
COPY third_party/sexyz/output-flush.patch /src/
RUN cd sbbs && git apply /src/output-flush.patch
WORKDIR /src/sbbs/src/sbbs3
# The version headers need git; generated first, they go into the
# source archive too, so it rebuilds without git or network.
RUN make git_branch.h git_hash.h && \
    cd /src/sbbs && tar czf /tmp/sexyz-source-${SBBS_COMMIT%${SBBS_COMMIT#????????}}.tar.gz \
        src/build src/sbbs3 src/xpdev src/hash src/smblib src/encode
RUN make RELEASE=1 sexyz && \
    install -m 0755 */sexyz /usr/local/bin/sexyz
COPY third_party/sexyz/NOTICE third_party/sexyz/output-flush.patch /out/doc/
RUN cp /src/sbbs/docs/gpl.txt /out/doc/COPYING && \
    cp /src/sbbs/docs/lgpl.txt /out/doc/COPYING.LESSER && \
    sed -n '1,/\*\//p' zmodem.c > /out/doc/LICENSE.zmodem && \
    sed -n '1,/\*\//p' ../hash/md5.c > /out/doc/LICENSE.md5 && \
    mv /tmp/sexyz-source-*.tar.gz /out/doc/

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
COPY --from=sexyz-build /usr/local/bin/sexyz /usr/local/bin/sexyz
COPY --from=sexyz-build /out/doc /usr/local/share/doc/sexyz
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
