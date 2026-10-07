#!/usr/bin/env bash
#
# Fetches your NullModem BBS QWK mail via HTTP, opens it in MultiMail
# (https://wmcbrine.com/MultiMail/, "mm") for reading/replying, then
# uploads whatever reply packet MultiMail produced.
#
# Requires: curl, mm (MultiMail -- "apt install multimail" on Debian/
# Ubuntu, or build from https://github.com/wmcbrine/MultiMail).
#
# ==================== Configuration ====================
# Edit these directly, or leave them blank and set the matching
# NULLMODEM_* environment variable instead (handy for a shared
# script or a scheduled job) -- an env var always wins if both are
# set. Leaving USERNAME/PASSWORD blank prompts for them instead.

BBS_URL="https://bbs.example.com"
USERNAME=""    # e.g. "alice"
PASSWORD=""    # leave blank to be prompted each run
QWK_DOWN=""    # leave blank for the default ($HOME/.nullmodem-qwk/incoming)
QWK_UP=""      # leave blank for the default ($HOME/.nullmodem-qwk/outgoing)
# =========================================================

set -euo pipefail

BBS_URL="${NULLMODEM_URL:-$BBS_URL}"
USERNAME="${NULLMODEM_USER:-$USERNAME}"
PASSWORD="${NULLMODEM_PASS:-$PASSWORD}"
WORKDIR="${NULLMODEM_QWK_DIR:-$HOME/.nullmodem-qwk}"
IN_DIR="${NULLMODEM_QWK_DOWN:-${QWK_DOWN:-$WORKDIR/incoming}}"
OUT_DIR="${NULLMODEM_QWK_UP:-${QWK_UP:-$WORKDIR/outgoing}}"

if [ -z "$USERNAME" ]; then
	read -rp "BBS username: " USERNAME
fi
if [ -z "$PASSWORD" ]; then
	read -rsp "BBS password: " PASSWORD
	echo
fi

mkdir -p "$IN_DIR" "$OUT_DIR"
rm -f "$IN_DIR"/*.qwk "$IN_DIR"/*.QWK

echo "Logging in to $BBS_URL ..."
TOKEN=$(curl -fsS -X POST "$BBS_URL/api/bbs/auth/login" \
	-H 'Content-Type: application/json' \
	-d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}" |
	sed -n 's/.*"token":"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
	echo "Login failed -- check username/password." >&2
	exit 1
fi

echo "Downloading QWK packet ..."
PACKET="$IN_DIR/packet.qwk"
HTTP_CODE=$(curl -sS -o "$PACKET" -w '%{http_code}' \
	-H "Authorization: Bearer $TOKEN" \
	"$BBS_URL/api/bbs/qwk/download")

if [ "$HTTP_CODE" = "204" ]; then
	echo "No new mail. Nothing to read."
	rm -f "$PACKET"
	exit 0
elif [ "$HTTP_CODE" != "200" ]; then
	echo "Download failed (HTTP $HTTP_CODE)." >&2
	cat "$PACKET" >&2 || true
	exit 1
fi

echo "Starting MultiMail -- reply packets are saved to $OUT_DIR"
rm -f "$OUT_DIR"/*.rep "$OUT_DIR"/*.REP
mm -PacketDir "$IN_DIR" -ReplyDir "$OUT_DIR" "$IN_DIR"

REPLY=$(find "$OUT_DIR" -maxdepth 1 -iname '*.rep' -print -quit)
if [ -z "$REPLY" ]; then
	echo "No reply packet was created -- nothing to upload."
	exit 0
fi

echo "Uploading reply packet ($REPLY) ..."
curl -fsS -X POST "$BBS_URL/api/bbs/qwk/upload" \
	-H "Authorization: Bearer $TOKEN" \
	-F "file=@$REPLY"
echo
echo "Done."
