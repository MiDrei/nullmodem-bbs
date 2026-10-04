# Backups

**English** · [Deutsch](backup.de.md)

Every night (default 03:00, before the maintenance) the web service writes a
backup to `data/backups/` — adjustable under Admin → System → Backups, where
you'll also find "Back up now", download and delete.

## What's in it

A `nullmodem-YYYYMMDD-HHMMSS.tar.gz` with the same paths as in the deploy
directory:

| Path | Contents |
|---|---|
| `data/nullmodem.sqlite` | the database: users, messages, netmail, areas, file list, logs … (a consistent copy via `VACUUM INTO`, while running) |
| `configs/bbs.yaml`, `configs/web.yaml` | the configuration, incl. uplink passwords |
| `configs/menus/`, `configs/screens/` | menus and ANSI screens (the ones edited in the designer too) |
| `data/lang/` | your changed texts (language editor) |
| `data/jwt_secret`, `data/ssh_host_key`, `data/vapid.json` … | keys: logins stay valid, the SSH host key stays the same, push subscriptions keep working |
| `data/files/`, `data/doors/` | only with "Include the file areas and doors" |

Not in it: BinkP transcripts and the inbound archive (the maintenance clears
them out after a few days anyway).

The backup contains passwords and keys — keep it as carefully as the machine
itself.

## Retention

The newest 7 (`keep_daily`) plus the newest of each of the last 4 weeks
(`keep_weekly`). Older ones are deleted after every backup.

The backups sit on the same disk as the BBS: they help against mistakes and a
broken database, not against a broken disk — for that, turn on the encrypted
off-site copy (below), download one now and then, or put the directory on
another disk or a NAS (mounted into the container, e.g. `/backup` in
`docker-compose.yml` for the `web` service, and entered as directory
`/backup`).

## Off-site copy (encrypted)

Admin → System → Backups → **Off-site copy**: every backup then also goes to a
storage service (S3, OpenStack Swift, SFTP or WebDAV) — encrypted with
[age](https://age-encryption.org) first. The BBS only knows the public key;
neither the service nor anyone who gets at the server can read the copies.
Only the private key opens them.

1. **Key:** "Make a key" — the private key (`AGE-SECRET-KEY-1…`) is shown
   **only once**. Download it or put it in your password manager, don't leave
   it on the server. Without it the copies are worthless. (Or paste your own
   public `age1…` key.)
2. **Destination:**
   - **OpenStack Swift** (e.g. Infomaniak Swiss Backup): auth URL
     (`https://swiss-backupNN.infomaniak.com/identity/v3`), user, password,
     project, region, container — the values are in the device's
     OpenRC/rclone configuration. The container is created if it doesn't
     exist.
   - **S3** (AWS, Hetzner Object Storage, Infomaniak, Wasabi, Backblaze B2,
     Exoscale, MinIO …): endpoint (e.g. `s3.amazonaws.com`,
     `fsn1.your-objectstorage.com`), region, bucket, access and secret key,
     optionally a prefix. The BBS creates the bucket if it doesn't exist;
     "path-style" only for services that require it (MinIO). Best use a key
     that may only access this bucket.
   - **WebDAV** (Nextcloud, ownCloud, kDrive, Storage Box): the folder's URL
     (Nextcloud: Settings → WebDAV, plus the folder name), user and — best —
     an app password. Missing folders are created.
   - **SFTP** (e.g. Hetzner Storage Box: host `uNNNNNN.your-storagebox.de`,
     port 23): user and password, or better "Make a key" and add the line
     shown to the box's `.ssh/authorized_keys`. On the first test the BBS
     shows the server's key for confirmation (compare it with what the
     provider states); from then on it only connects to exactly that server.
3. **Save and test** writes, reads and deletes a small file. Then turn on
   "after each backup"; "Copy the newest now" sends one right away.

The newest 14 are kept there plus the newest of each of the last 8 weeks
(adjustable). If a copy fails, the BBS retries every hour and reports it
under "Needs attention".

**Getting a copy back:** download it (the service's web interface, `rclone`,
`sftp` …), then decrypt:

```sh
age -d -i nullmodem-backup-key.txt nullmodem-YYYYMMDD-HHMMSS.tar.gz.age > nullmodem-YYYYMMDD-HHMMSS.tar.gz
```

(`age` exists for Linux, macOS and Windows, e.g. `apt install age`.) Then
restore as below.

## Restoring

In the deploy directory (on apollo `~/nullmodem-deploy`):

```sh
docker compose down
# move the current state out of the way, to be safe
mv data/nullmodem.sqlite data/nullmodem.sqlite.before-restore
rm -f data/nullmodem.sqlite-wal data/nullmodem.sqlite-shm
tar xzf data/backups/nullmodem-20261001-030000.tar.gz
docker compose up -d
```

`tar` only overwrites what's in the backup. Removing `-wal`/`-shm` matters:
they belong to the old database.

Restoring single parts works too, e.g. only the screens:

```sh
tar xzf data/backups/nullmodem-….tar.gz configs/screens
```
