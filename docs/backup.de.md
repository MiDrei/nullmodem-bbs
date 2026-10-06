# Backups

[English](backup.md) · **Deutsch**

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
| `data/lang/` | deine geänderten Texte (Spracheditor) |
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
Fehler und eine kaputte Datenbank, nicht gegen eine kaputte Platte —
dafür die verschlüsselte Kopie ausser Haus (unten) einschalten, ab und zu
eines herunterladen, oder das Verzeichnis auf eine andere Platte bzw. ein
NAS legen (im Container gemountet, z. B. `/backup` in der
`docker-compose.yml` beim Dienst `web` und als Verzeichnis `/backup`
eintragen).

## Prüfung

Jedes neue Backup — nächtlich oder „Back up now“ — wird gleich nach dem
Schreiben geprüft: Das Archiv wird bis zum Ende gelesen (die Prüfsumme von
gzip erkennt eine beschädigte Datei), die Datenbank darin in eine temporäre
Datei entpackt und muss die Integritätsprüfung von SQLite bestehen,
`bbs.yaml` muss drin sein, und es muss mindestens die Hälfte der Benutzer und
Nachrichten der BBS enthalten (eine leere oder falsche Datenbank tut das
nicht). Das Ergebnis steht auf der Backups-Seite („Check now“ wiederholt es)
und im Dashboard; eine fehlgeschlagene Prüfung erscheint unter „Needs
attention“ und als Push.

## Kopie ausser Haus (verschlüsselt)

Admin → System → Backups → **Off-site copy**: Jedes Backup geht danach
zusätzlich an einen Speicherdienst (S3, OpenStack Swift, SFTP oder WebDAV) — vorher mit [age](https://age-encryption.org)
verschlüsselt. Die BBS kennt nur den öffentlichen Schlüssel; weder der
Dienst noch jemand, der an den Server kommt, kann die Kopien lesen. Nur der
private Schlüssel öffnet sie.

1. **Schlüssel:** „Make a key“ — der private Schlüssel
   (`AGE-SECRET-KEY-1…`) wird **nur einmal** angezeigt. Herunterladen bzw.
   in den Passwort-Manager, nicht auf dem Server lassen. Ohne ihn sind die
   Kopien wertlos. (Oder einen eigenen öffentlichen `age1…`-Schlüssel
   einfügen.)
2. **Ziel:**
   - **OpenStack Swift** (z. B. Infomaniak Swiss Backup): Auth-URL
     (`https://swiss-backupNN.infomaniak.com/identity/v3`), Benutzer,
     Passwort, Projekt, Region, Container — die Werte stehen in der
     OpenRC-/rclone-Konfiguration des Geräts. Der Container wird angelegt,
     falls es ihn nicht gibt.
   - **S3** (AWS, Hetzner Object Storage, Infomaniak, Wasabi, Backblaze B2,
     Exoscale, MinIO …): Endpoint (z. B. `s3.amazonaws.com`,
     `fsn1.your-objectstorage.com`), Region, Bucket, Access- und Secret-Key,
     optional ein Präfix. Den Bucket legt die BBS an, falls es ihn nicht gibt;
     „path-style“ nur für Dienste, die es verlangen (MinIO). Am besten einen
     Schlüssel, der nur diesen Bucket darf.
   - **WebDAV** (Nextcloud, ownCloud, kDrive, Storage Box): die URL des Ordners
     (Nextcloud: Einstellungen → WebDAV, plus Ordnername), Benutzer und — am
     besten ein App-Passwort. Fehlende Ordner werden angelegt.
   - **SFTP** (z. B. Hetzner Storage Box: Host `uNNNNNN.your-storagebox.de`,
     Port 23): Benutzer und Passwort, oder besser „Make a key“ und die
     angezeigte Zeile in `.ssh/authorized_keys` der Box eintragen. Beim
     ersten Test zeigt die BBS den Schlüssel des Servers zum Bestätigen
     (mit den Angaben des Anbieters vergleichen); danach verbindet sie sich
     nur noch mit genau diesem Server.
3. **Save and test** schreibt, liest und löscht eine kleine Datei. Dann
   „after each backup“ einschalten; „Copy the newest now“ schickt sofort
   eines.

Dort bleiben die neuesten 14 plus je das neueste der letzten 8 Wochen
(einstellbar). Schlägt eine Kopie fehl, versucht es die BBS stündlich
wieder und meldet es unter „Needs attention“.

**Eine Kopie zurückholen:** herunterladen (Webinterface des Dienstes,
`rclone`, `sftp` …), dann entschlüsseln:

```sh
age -d -i nullmodem-backup-key.txt nullmodem-JJJJMMTT-HHMMSS.tar.gz.age > nullmodem-JJJJMMTT-HHMMSS.tar.gz
```

(`age` gibt es für Linux, macOS und Windows, z. B. `apt install age`.)
Danach wie unten zurückspielen.

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
