# Backups

Der Web-Dienst schreibt jede Nacht (Standard 03:00, vor der Maintenance)
ein Backup nach `data/backups/` — einstellbar unter Admin → System →
Backups, dort auch „Back up now“, Download und Löschen.

## Was drin ist

Ein `nullmodem-JJJJMMTT-HHMMSS.tar.gz` mit denselben Pfaden wie im
Deploy-Verzeichnis:

| Pfad | Inhalt |
|---|---|
| `data/nullmodem.sqlite` | die Datenbank: Benutzer, Nachrichten, Netmail, Areas, Dateiliste, Logs … (konsistente Kopie per `VACUUM INTO`, im laufenden Betrieb) |
| `configs/bbs.yaml`, `configs/web.yaml` | die Konfiguration, inkl. Uplink-Passwörter |
| `configs/menus/`, `configs/screens/` | Menüs und ANSI-Bildschirme (auch die im Designer bearbeiteten) |
| `data/jwt_secret`, `data/ssh_host_key`, `data/vapid.json` … | Schlüssel: Logins bleiben gültig, der SSH-Hostkey bleibt gleich, Push-Abos bleiben bestehen |
| `data/files/`, `data/doors/` | nur mit „Include the file areas and doors“ |

Nicht drin: BinkP-Mitschnitte und das Eingangsarchiv (die Maintenance
räumt sie ohnehin nach wenigen Tagen weg).

Das Backup enthält Passwörter und Schlüssel — so aufbewahren wie die
Maschine selbst.

## Aufbewahrung

Die neuesten 7 (`keep_daily`) plus das neueste jeder der letzten 4
Wochen (`keep_weekly`). Ältere werden nach jedem Backup gelöscht.

Die Backups liegen auf derselben Platte wie die BBS: Sie helfen gegen
Fehler und eine kaputte Datenbank, nicht gegen eine kaputte Platte. Ab
und zu eines herunterladen, oder das Verzeichnis auf eine andere Platte
bzw. ein NAS legen (im Container gemountet, z. B. `/backup` in der
`docker-compose.yml` beim Dienst `web` und als Verzeichnis `/backup`
eintragen).

## Zurückspielen

Im Deploy-Verzeichnis (auf apollo `~/nullmodem-deploy`):

```sh
docker compose down
# zur Sicherheit den aktuellen Stand wegkopieren
mv data/nullmodem.sqlite data/nullmodem.sqlite.vor-restore
rm -f data/nullmodem.sqlite-wal data/nullmodem.sqlite-shm
tar xzf data/backups/nullmodem-20261001-030000.tar.gz
docker compose up -d
```

`tar` überschreibt dabei nur, was im Backup steht. Wichtig ist das
Entfernen von `-wal`/`-shm`: sie gehören zur alten Datenbank.

Nur einzelne Teile zurückholen geht auch, z. B. nur die Bildschirme:

```sh
tar xzf data/backups/nullmodem-….tar.gz configs/screens
```
