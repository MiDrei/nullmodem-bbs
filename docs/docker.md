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

`configs/bbs.yaml` and `configs/web.yaml` are gitignored (the web
admin Settings page can write a live BinkP uplink password into
`bbs.yaml`) and so aren't baked into the image. `docker-compose.yml`
bind-mounts the whole `./configs` directory into every container;
without a real `bbs.yaml`/`web.yaml` there, every daemon just falls
back to its own built-in defaults (same as running the binaries
directly). Copy `configs/bbs.yaml.example` to get started.

`data/` (SQLite database, SSH host key, JWT signing key, uploaded
files, and any door installs -- see `docs/adding-a-door.md`) is a
named volume (`nullmodem-data`) shared by all three containers, so it
survives a `docker compose down`/recreate. All three daemons write to
the same SQLite database concurrently via WAL mode (see
`internal/db`) -- this is the same design the non-containerized
binaries already use, not something specific to Docker.

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
docker run -d --name nullmodem-bbs \
    -p 2323:2323 -p 2222:2222 \
    -v "$PWD/configs:/app/configs" \
    -v nullmodem-data:/app/data \
    nullmodem-bbs ./bin/bbs
```

Swap the last line's `./bin/bbs` for `./bin/mailer` or `./bin/web`
(with that service's own ports) to run the other two daemons the same
way.
