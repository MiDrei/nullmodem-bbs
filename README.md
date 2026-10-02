# NullModem BBS

Eine Mailbox im Stil von Synchronet, Mystic und ENiGMA½ — über Telnet und
SSH mit ANSI/CP437 wie in den Neunzigern, dazu ein Web-Portal für Benutzer,
eine Web-Administration für den Sysop und ein FTN-Mailer für FidoNet, fsxNet
und Co. Geschrieben in Go, Daten in SQLite, Oberfläche in SvelteKit; läuft
als Docker-Image.

Teil der NullModem-Familie:

| Repo | Inhalt |
|---|---|
| **bbs** (dieses) | die Mailbox selbst |
| [kit](https://git.maik.ch/nullmodem/kit) | gemeinsamer Unterbau: ANSI/CP437, QWK/QWKE, Zmodem |
| [reader](https://git.maik.ch/nullmodem/reader) | NullModem Reader (`nmr`), Offline-Reader für QWK-Post dieser BBS |

## Was sie kann

**Telnet/SSH**
- ANSI-Bildschirme mit Platzhaltern (`{BBSNAME}`, `{USERNAME}`, `{FILL:x}` …),
  Menüs aus YAML, Lightbar-Listen, Security Levels 0–255
- Nachrichtenbereiche (lokal und Echomail), Netmail, Dateibereiche — mit
  Nachrichtensuche (Telnet, Portal, Reader), „neue Dateien“ über alle Areas
  und Dateisuche
- Unter den Anrufern: One-liner-Wand nach dem Login, Teleconference (Chat),
  Node-Nachrichten an andere Online-Anrufer (bei „Who's online“), Sysop rufen
  („Page“): der Sysop bekommt eine Push-Nachricht und antwortet im Web-Admin
  (Chat & One-liners) oder auf einem Node
- Abstimmungen (V): der Sysop fragt im Web-Admin, Anrufer stimmen über Telnet
  oder im Portal (Community) ab und sehen das Ergebnis als Balken; BBS-Liste (B),
  von den Anrufern gepflegt
- Nodelisten (I): werden aus den File-Echos der Netze übernommen (FSXNET.Z75 …),
  zum Nachschlagen und zum Prüfen von Netmail-Adressen („→ Agency BBS, Dunedin“)
- Vollbild-Editor zum Schreiben (Pfeiltasten, Pos1/Ende, Bild auf/ab, Wortumbruch,
  Antwort mit Zitat; ^Z speichern, ^X abbrechen, ^Y Zeile löschen); wer lieber
  zeilenweise schreibt, stellt im Profil den Zeileneditor ein
- Neue Nachrichten nach dem Login: Übersicht (Netmail, an dich, neu pro Area),
  „Read new messages“ liest alle Areas der Reihe nach, „Messages to you“ nur
  die an dich; welche Areas, bestimmt dieselbe Auswahl wie für QWK
- Datei-Download und -Upload per Zmodem (Synchronets `sexyz`)
- QWK-Pakete holen und Antworten hochladen, Auswahl der Bereiche
- Remote-Doors über RLogin (Door-Netzwerke wie DoorParty, andere BBS):
  Host und die beiden Benutzernamen mit Platzhaltern im Web-Admin
- Doors: native Linux-Doors per `DOOR32.SYS` und DOS-Doors unter DOSBox-X (DOOR.SYS, DORINFO1.DEF, DOORFILE.SR), verwaltet im Web-Admin mit Vorlagen (per Klick installierbar: MRC Chat (uMRC), Immortal Barons, Usurper, Usurper Reborn, Judge Dredd; vorbereitet: LORD, TradeWars, OO2, DoorMUD); tägliche Wartung pro Door (zur eingestellten Zeit, nie während jemand spielt)
- Profil: Realname, Zeitzone, Passwort, QWK-Einstellungen
- Wer sich als Erster registriert, wird Sysop

**Web** (`:8090`)
- Öffentliche Startseite (`/`): Begrüßungsbildschirm, alle Zugänge (Web-Terminal,
  Telnet/SSH, Portal, Reader-App, QWK), wer online ist, letzte Anrufer,
  One-Liner, Doors, FTN-Adressen, Statistik der letzten 30 Tage; Link-Vorschau
  (OpenGraph) mit dem Begrüßungsbildschirm als Bild; RSS-Feed pro öffentlicher
  Area (abschaltbar)
- Threads: Antworten über REPLY-Kludge, direkte Antworten oder den Betreff
  verknüpft; Thread-Ansicht im Portal und in der Reader-App, Thread-Liste (T)
  und `[`/`]` im Telnet, REPLY in ausgehender Echomail, Referenznummer im QWK-Paket
- Chat-Räume (`/rooms`, `/join` in der Teleconference) mit Brücken zu
  Discord-Kanälen und Matrix-Räumen: BBS-Anrufer erscheinen dort unter ihrem
  Namen, die anderen in der BBS als `name@discord` / `name@matrix`; nur
  ausgehende Verbindungen
- Meine Areas: jeder Anrufer nimmt Areas aus New-Scan, QWK und Reader-App heraus
  (Telnet K, Portal ✓, Reader); neue Areas sind automatisch drin
- Chat auch im Portal und in der Reader-App; öffentliche Download-Links für
  Dateien freigegebener Areas (Seite mit Link-Vorschau); Door-Bulletins
  (Scoreboards, News) im Doors-Menü, Portal und auf der Startseite
- BBS-Liste mit stündlichem Online-Check; Monatsrückblick per Netmail an die
  Sysops
- Menü-Editor (Admin → Content → Menus): Punkte, Aktionen, SL, neue Menüs,
  Vorschau wie im Telnet mit Abgleich gegen den Bildschirm, neue Standardpunkte
  einer Version per Klick; Änderungen gelten ohne Neustart
- Statistik (Admin → System → Statistics): Anrufe, Schreiber, Areas,
  Echomail pro Netzwerk, Doors, Downloads, BinkP-Sitzungen
- Portal für Benutzer (`/login`, `/message-areas` …): Nachrichten, Netmail, Dateien, QWK, Profil —
  dieselben Funktionen wie über Telnet
- Web-Terminal (`/terminal`): die BBS im Browser, ohne Telnet-Client — mit dem
  Pixel-Font der ANSI-Bildschirme, Tastenleiste fürs Handy; Sperren und
  Verbindungslimit gelten für die echte Adresse des Besuchers
- Reader fürs Handy (`/reader`): schlanke Web-App zum Lesen und Beantworten
  von Echomail und Netmail, installierbar auf dem Home-Bildschirm (iOS,
  Android); Gelesen-Status wie überall auf der BBS. Offline lesen (Ungelesenes
  wird vorab geholt, offline Geschriebenes später verschickt) und
  Benachrichtigungen (Web Push) bei neuer Netmail und Echomail an einen selbst
- Administration für den Sysop (`/admin`, ab SL 255): Benutzer, Bereiche,
  Security-Level-Matrix, Logs, BinkP-Uplinks, Areafix, Archiv, ANSI-Designer
  für die Bildschirme
- Schutz (Admin → Users → Security): Adressen mit zu vielen Fehl-Logins
  (Telnet, SSH, Portal, Admin) werden gesperrt, bei Wiederholung länger;
  Limit gleichzeitiger Verbindungen pro Adresse; Allow- und Blocklisten (IP oder
  Bereich). Neue Benutzer warten auf Freischaltung (lesen und Netmail an den
  Sysop dürfen sie schon), Push an den Sysop bei Neuanmeldung; gesperrte Handles
- Zwei-Faktor-Login (TOTP) für Admin und Telnet-Sysop-Menü, Passwort-Reset für
  Benutzer, Warnungen per Push (Dienst steht, Uplink unerreichbar, Backup fehlt,
  Platte voll, Netmail hängt) — siehe [docs/security.md](docs/security.md)
- Nächtliches Backup (Admin → Backups): Datenbank, Konfiguration, Menüs,
  Bildschirme und Schlüssel als ein `.tar.gz`, wahlweise mit Dateien und Doors;
  Download im Admin, Zurückspielen siehe [docs/backup.md](docs/backup.md)
- REST-API; die QWK-Endpunkte nutzen auch NullModem Reader und die Skripte in
  `scripts/multimail/`

**FTN**
- BinkP-Mailer (binkp/1.1), eingehend und ausgehend, Crash und Hold, mehrere
  AKAs
- Tosser für Echomail, Netmail und TIC-Datei-Echos
- Areafix/Filefix, auch für eigene Downlinks
- TIC „Replaces“: eine Datei ersetzt ältere in der Area (auch mit Platzhaltern),
  weitergeleitet wird die Angabe mit
- Maintenance (Admin → Maintenance): räumt nachts alte Echomail, Dateien, gelesene
  Netmail, Log, BinkP-Mitschnitte und Eingangsarchiv auf und verdichtet die
  Datenbank; Grenzen pro Area, Vorschau vor dem Löschen
- InterBBS Last Callers: liest die Liste aus FSX_DAT (beide gängigen Formate),
  zeigt sie nach dem Login und im Portal und meldet die eigenen Anrufer;
  Daten-Areas wie FSX_DAT lassen sich für Anrufer ausblenden
- Points, z. B. ein Reader wie FidoMail; wahlweise schreibt er als dein
  BBS-User, als käme die Post direkt von der BBS

## Aufbau

Drei unabhängige Dienste, alle aus demselben Image, verbunden nur über die
gemeinsame SQLite-Datenbank und `configs/bbs.yaml`:

| Dienst | Programm | Ports |
|---|---|---|
| `bbs` | `cmd/bbs` | 2323 Telnet, 2222 SSH |
| `mailer` | `cmd/mailer` | 24554 BinkP |
| `web` | `cmd/web` | 8090 Portal, Administration, API |

Wer nicht am FTN hängt, lässt `mailer` einfach weg.

## Schnellstart mit Docker

```sh
cp configs/bbs.yaml.example configs/bbs.yaml
docker compose up -d --build
telnet localhost 2323        # als Erster registrieren = Sysop
```

Danach unter `http://localhost:8090/admin` mit demselben Konto anmelden.
Konfiguration, Daten-Verzeichnis, eigene Bildschirme, Multi-Arch-Builds und
versionierte Images: [docs/docker.md](docs/docker.md).

## Entwickeln

```sh
go build -o bin/ ./cmd/...           # bbs, mailer, web
(cd web && npm ci && npm run build)  # Oberfläche nach web/build
./bin/bbs & ./bin/web &              # liest configs/bbs.yaml und configs/web.yaml
go test ./...
```

- Go 1.26+, Node 24 für die Oberfläche; alles ohne cgo, auch SQLite
  (`modernc.org/sqlite`).
- Für Zmodem braucht es `sexyz` im `PATH` — auch für die Zmodem-Tests, die
  sonst übersprungen werden: [docs/building-sexyz.md](docs/building-sexyz.md).
- Das Kit wird über eine feste Version in `go.mod` eingebunden. Wer BBS und
  Kit gleichzeitig ändert, legt BBS, Kit und Reader nebeneinander und nutzt
  das `go.work` im Elternverzeichnis; Kit-Releases beschreibt die README des
  Kits.
- Die Versionsnummer steht in `internal/version/version.go` und erscheint im
  Begrüßungsbildschirm.

## Weitere Dokumentation

- [docs/handbook.md](docs/handbook.md) — **Sysop-Handbuch**: Aufsetzen, Netzwerke,
  Areas, Doors, Benutzer, Alltag, Updates, Fehlersuche
- [docs/docker.md](docs/docker.md) — Betrieb mit Docker
- [docs/adding-a-door.md](docs/adding-a-door.md) — Doors einrichten, nativ und
  unter DOSBox-X
- [docs/points.md](docs/points.md) — Points und eigene Reader-Apps (FidoMail)
- [docs/building-sexyz.md](docs/building-sexyz.md) — `sexyz` bauen, warum nicht
  lrzsz
- [docs/third-party.md](docs/third-party.md) — Fremdsoftware im Image und ihre
  Lizenzen
- [scripts/multimail/README.md](scripts/multimail/README.md) — QWK-Austausch
  per Skript, z. B. für MultiMail

## Fremdsoftware

Das Docker-Image enthält Synchronets `sexyz` (GNU GPL, Version 2 oder später)
als eigenes Programm für Zmodem-Übertragungen; Lizenztexte, Hinweise und der
vollständige Quellcode liegen im Image unter `/usr/local/share/doc/sexyz/`.
Einzelheiten: [docs/third-party.md](docs/third-party.md).

## Lizenz

MIT — siehe [LICENSE](LICENSE). Fremdsoftware im Docker-Image behält ihre
eigene Lizenz (siehe oben).
