# Running NullModem BBS in Docker

**English** · [Deutsch](docker.de.md)

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
files, door installs -- see `docs/adding-a-door.md` -- and the
changed texts under `data/lang/`) is also
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

## Restarting from the web admin

**Admin → System → Services** lists the three daemons (bbs, mailer,
web) with their version and uptime, and restarts them. There's no
Docker socket involved: each daemon keeps a heartbeat in the database
and watches it for a restart request, then exits on its own -- and the
containers' `restart: unless-stopped` starts it again within seconds.
The mailer finishes its poll round and any inbound sessions first; the
BBS can wait until nobody is online ("Restart when idle").

Settings a daemon only reads at startup mark it when saved (BinkP
settings and networks -> mailer; Telnet/SSH, the new-user level, menus
and `welcome.ans` -> bbs; the FTN addresses -> all three), and a banner
in the admin offers the restart. Everything else -- doors, areas, other
screens -- applies right away.

Without Docker (running the binaries by hand), a daemon stopped this
way has to be started again yourself.

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
docker buildx build --platform linux/amd64,linux/arm64 --build-arg COMMIT=$(git rev-parse --short HEAD) -t nullmodem-bbs:latest .
```

`--build-arg COMMIT=…` stamps the commit into the binaries; it shows
beside the version (welcome screen, `?` command, admin dashboard,
Services, page footers) together with the build time. Without it only
the build time shows.

## Building without Compose

```sh
docker build --build-arg COMMIT=$(git rev-parse --short HEAD) -t nullmodem-bbs .
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

Each release tag (`vX.Y.Z`) on GitHub builds the image for `amd64` and
`arm64` and publishes it to the GitHub Container Registry with that tag
and `latest`:

```sh
docker pull ghcr.io/midrei/nullmodem-bbs:vX.Y.Z
```

Use the image in `docker-compose.yml`'s `image:` lines instead of
building it yourself. Pin a deployment to a version tag rather than
`latest`, and move on deliberately with
`docker compose pull && docker compose up -d` once a new one exists --
rolling back is the same with the previous tag.

## Third-party software

The image carries Synchronet's `sexyz` for Zmodem transfers, built from
source and licensed under the GNU GPL (v2 or later) -- its license
texts, notices and the exact source are in
`/usr/local/share/doc/sexyz/` inside the image. See
[third-party.md](third-party.md).
