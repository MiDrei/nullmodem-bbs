# Running NullModem BBS in Docker

## Quick start

```sh
cp configs/bbs.yaml.example configs/bbs.yaml    # optional -- see below
docker compose up -d --build
```

This starts three containers from one built image (see `Dockerfile`):

| Service  | Binary        | Ports                          |
|----------|---------------|---------------------------------|
| `bbs`    | `cmd/bbs`     | `2323` (telnet), `2222` (ssh)   |
| `mailer` | `cmd/mailer`  | `24554` (inbound BinkP)         |
| `web`    | `cmd/web`     | `8090` (admin UI + REST API)    |

They're independent -- stop/restart/scale any one without affecting
the others (`docker compose restart mailer`, `docker compose stop
mailer` if this system isn't on FidoNet/fsxNet, etc.), same as running
them as three separate OS processes on bare metal.

## Config and data

`configs/bbs.yaml` is gitignored (the web admin Settings page can
write a live BinkP uplink password into it) and so isn't baked into
the image; `configs/web.yaml` is tracked but still deployment-
specific. `docker-compose.yml` bind-mounts each individually
(`./configs/bbs.yaml`, `./configs/web.yaml`) -- without a real
`bbs.yaml` there, every daemon just falls back to its own built-in
defaults (same as running the binaries directly). Copy
`configs/bbs.yaml.example` to get started, and note a *file* bind
mount needs the source file to already exist, or Docker creates an
empty directory there instead.

`configs/menus` and `configs/screens` are bind-mounted too, but as
whole directories, and for a different reason than bbs.yaml: they
ship with working defaults baked into the image (`configs-defaults/`
inside the container), but the web admin's ANSI Designer writes .ans
files straight back into `configs/screens` at runtime (see
`internal/web/screens_handler.go`), and a sysop can hand-edit
`configs/menus` too -- both need to survive a container recreate or
image update the same way `data/` does, or a redeploy would silently
wipe a sysop's customizations back to stock. `docker-entrypoint.sh`
reconciles this on every startup: it copies each default file into
the bind-mounted directory *only if a file by that name isn't already
there* (`cp -rn`), so a first-ever run (empty bind mount) gets the
full default set, a later image that adds a new default screen adds
just that one file, and anything a sysop already customized is never
touched. A fresh checkout's `configs/menus`/`configs/screens` (used
for local, non-Docker runs) are the same files this seeding starts
from -- see `internal/bbs`'s own doc comments for how the plain
binaries load them.

`data/` (SQLite database, SSH host key, JWT signing key, uploaded
files, and any door installs -- see `docs/adding-a-door.md`) is also
a plain bind mount (`./data`), not a named volume, so it survives a
`docker compose down`/recreate and -- unlike a named volume -- is
directly reachable from the host: installing a door is just copying
files into `./data/doors/`, no `docker cp`/`docker exec` needed. All
three daemons write to the same SQLite database concurrently via WAL
mode (see `internal/db`) -- this is the same design the
non-containerized binaries already use, not something specific to
Docker.

The containers run as root by default (see `Dockerfile`), which would
otherwise leave everything under `./data` owned by root on the host.
`docker-compose.yml` overrides this with `user: "${PUID:-1000}:${PGID:-1000}"`
-- set these to your own host user before first start so files the
containers create (and doors you drop in yourself) have matching
ownership on both sides:

```sh
mkdir -p data
echo "PUID=$(id -u)" >> .env
echo "PGID=$(id -g)" >> .env
docker compose up -d --build
```

If `./data` already has content owned by root from an earlier run
without PUID/PGID set (or from a migration off a named volume), fix
ownership once with `sudo chown -R $(id -u):$(id -g) data`.

## Image size and DOS door support

The runtime image is `debian:trixie-slim`, not alpine -- classic DOS
door support (`kind: dosbox` in `configs/bbs.yaml`, see
`docs/adding-a-door.md`) needs a real `dosbox-x` package with its own
shared-library dependencies (SDL2, an audio stack for its FluidSynth
MIDI emulation, etc.), which is what makes up most of the image's size
(~550MB). `dosbox-x` isn't packaged for Debian 12 (bookworm) at all,
which is why the base is trixie specifically. Doors themselves are
runtime state under `data/doors/` (the shared volume above), not part
of the image -- a fresh container has no doors configured until a
sysop installs one there and points `configs/bbs.yaml` at it.

Door sessions run DOSBox-X fully headless (`SDL_VIDEODRIVER=dummy`,
set in the Dockerfile) -- no X server or virtual framebuffer needed
inside the container; see `docs/adding-a-door.md` for why this works
reliably for the `-socket`/`inhsocket:1` mechanism this project uses,
where it wouldn't for DOSBox-X's plain TCP-based nullmodem modes.

## Multi-arch builds

The Go build stage cross-compiles via `TARGETOS`/`TARGETARCH` (set
automatically by BuildKit) with `CGO_ENABLED=0` -- every dependency,
`modernc.org/sqlite` included, is pure Go, so no C toolchain or
per-arch base image juggling is needed there. `dosbox-x` is available
from Debian's own repos for both `amd64` and `arm64`. Build both in
one pass with buildx:

```sh
docker buildx build --platform linux/amd64,linux/arm64 -t nullmodem-bbs:latest .
```

## Building without Compose

```sh
docker build -t nullmodem-bbs .
mkdir -p data configs/menus configs/screens
docker run -d --name nullmodem-bbs \
    -p 2323:2323 -p 2222:2222 \
    --user "$(id -u):$(id -g)" \
    -v "$PWD/configs/bbs.yaml:/app/configs/bbs.yaml" \
    -v "$PWD/configs/menus:/app/configs/menus" \
    -v "$PWD/configs/screens:/app/configs/screens" \
    -v "$PWD/data:/app/data" \
    nullmodem-bbs ./bin/bbs
```

Swap the last line's `./bin/bbs` for `./bin/mailer` or `./bin/web`
(with that service's own ports, and `web` also needs
`-v "$PWD/configs/web.yaml:/app/configs/web.yaml"`) to run the other
two daemons the same way; `mailer` doesn't serve the BBS UI or
Designer, so it doesn't need the `configs/menus`/`configs/screens`
mounts at all.

## Versioned image tags

Every image pushed to the registry gets both a floating `latest` tag
and one pinned to `internal/version.Version`'s `vX.Y.Z[-dev]` suffix
(the same string the welcome screen shows -- see
`configs/screens/welcome.ans`), so a production deploy can pin to,
and roll back to, a specific build instead of always tracking
whatever was pushed last:

```sh
VTAG=$(grep -oP 'v[0-9]+\.[0-9]+\.[0-9]+(-dev)?' internal/version/version.go)
docker build -t nullmodem-bbs:latest .
docker tag nullmodem-bbs:latest git.maik.ch/maik.ch/nullmodem:latest
docker tag nullmodem-bbs:latest git.maik.ch/maik.ch/nullmodem:"$VTAG"
docker push git.maik.ch/maik.ch/nullmodem:latest
docker push git.maik.ch/maik.ch/nullmodem:"$VTAG"
```

A deployment (e.g. apollo's `~/nullmodem-deploy/docker-compose.yml`)
references the specific `vX.Y.Z[-dev]` tag in its `image:` lines, not
`latest`, and moves forward deliberately with
`docker compose pull && docker compose up -d` once a new tag exists --
never edited directly otherwise, per this project's deploy workflow
(develop and commit locally, roll out to production only via a
freshly pushed image).
