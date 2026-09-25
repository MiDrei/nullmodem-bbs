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
- Nachrichtenbereiche (lokal und Echomail), Netmail, Dateibereiche
- Datei-Download und -Upload per Zmodem (Synchronets `sexyz`)
- QWK-Pakete holen und Antworten hochladen, Auswahl der Bereiche
- Doors: native Linux-Doors per `DOOR32.SYS` und DOS-Doors unter DOSBox-X
- Profil: Realname, Zeitzone, Passwort, QWK-Einstellungen
- Wer sich als Erster registriert, wird Sysop

**Web** (`:8090`)
- Portal für Benutzer (`/`): Nachrichten, Netmail, Dateien, QWK, Profil —
  dieselben Funktionen wie über Telnet
- Administration für den Sysop (`/admin`, ab SL 255): Benutzer, Bereiche,
  Security-Level-Matrix, Logs, BinkP-Uplinks, Areafix, Archiv, ANSI-Designer
  für die Bildschirme
- REST-API; die QWK-Endpunkte nutzen auch NullModem Reader und die Skripte in
  `scripts/multimail/`

**FTN**
- BinkP-Mailer (binkp/1.1), eingehend und ausgehend, Crash und Hold, mehrere
  AKAs
- Tosser für Echomail, Netmail und TIC-Datei-Echos
- Areafix/Filefix, auch für eigene Downlinks

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

- [docs/docker.md](docs/docker.md) — Betrieb mit Docker
- [docs/adding-a-door.md](docs/adding-a-door.md) — Doors einrichten, nativ und
  unter DOSBox-X
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
