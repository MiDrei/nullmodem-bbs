#!/bin/sh
# Seeds ./configs/menus and ./configs/screens (bind-mounted, see
# docker-compose.yml) from this image's own baked-in defaults
# (configs-defaults/) without ever overwriting a file that's already
# there -- a sysop's edits via the web admin's ANSI Designer (see
# internal/web/screens_handler.go) or a hand-edited menu must survive
# a newer image being deployed. A first-ever run (empty bind mount)
# gets the full default set; a later image that adds a new screen/menu
# nobody has customized yet gets that one file added too, but nothing
# already customized is touched. Set -e is deliberately NOT used here:
# a missing/unwritable bind mount shouldn't block startup, it should
# just leave that screen/menu unseeded and let the daemon fall back to
# its own built-in defaults the way it already does for a missing
# bbs.yaml/web.yaml -- this also covers cmd/mailer, which doesn't get
# configs/menus or configs/screens bind-mounted at all (it doesn't
# serve the BBS UI or Designer), so configs/ itself isn't writable
# there and both mkdir and cp are expected to no-op, silently.
mkdir -p configs/menus configs/screens 2>/dev/null
cp -rn configs-defaults/menus/. configs/menus/ 2>/dev/null
cp -rn configs-defaults/screens/. configs/screens/ 2>/dev/null

exec "$@"
