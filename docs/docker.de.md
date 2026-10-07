# NullModem BBS mit Docker betreiben

[English](docker.md) · **Deutsch**

## Schnellstart

```sh
cp configs/bbs.yaml.example configs/bbs.yaml    # optional -- siehe unten
docker compose up -d --build
```

Das startet drei Container aus einem gebauten Image (siehe `Dockerfile`):

| Dienst   | Programm      | Ports                           |
|----------|---------------|---------------------------------|
| `bbs`    | `cmd/bbs`     | `2323` (Telnet), `2222` (SSH)   |
| `mailer` | `cmd/mailer`  | `24554` (eingehend BinkP)       |
| `web`    | `cmd/web`     | `8090` (Admin, Portal, REST-API) |

Sie sind voneinander unabhängig -- jeder lässt sich stoppen oder neu
starten, ohne die anderen zu berühren (`docker compose restart mailer`,
`docker compose stop mailer`, wenn das System nicht an FidoNet/fsxNet
hängt usw.), genau wie drei getrennte Prozesse ohne Docker.

## Konfiguration und Daten

`configs/bbs.yaml` ist nicht im Git (die Einstellungsseite im Web-Admin
schreibt z. B. ein BinkP-Uplink-Passwort hinein) und deshalb nicht ins
Image eingebaut; `configs/web.yaml` ist im Git, aber trotzdem pro
Installation verschieden. `docker-compose.yml` bindet beide einzeln ein
(`./configs/bbs.yaml`, `./configs/web.yaml`) -- ohne echte `bbs.yaml`
nimmt jeder Dienst seine eingebauten Standardwerte (wie beim direkten
Start der Programme). `configs/bbs.yaml.example` als Ausgangspunkt
kopieren. Achtung: Ein Bind-Mount einer *Datei* braucht die Datei
schon vorher, sonst legt Docker an ihrer Stelle ein leeres Verzeichnis
an.

`configs/menus` und `configs/screens` sind ebenfalls eingebunden, aber
als ganze Verzeichnisse und aus einem anderen Grund: Sie kommen mit
funktionierenden Standarddateien im Image (`configs-defaults/` im
Container), doch der ANSI-Designer im Web-Admin schreibt .ans-Dateien
zur Laufzeit direkt nach `configs/screens` (siehe
`internal/web/screens_handler.go`), und der Sysop kann `configs/menus`
auch von Hand ändern -- beides muss ein Neuerstellen des Containers und
ein Image-Update überleben wie `data/`, sonst würde ein Deploy die
Anpassungen still auf den Auslieferungszustand zurücksetzen.
`docker-entrypoint.sh` gleicht das bei jedem Start ab: Es kopiert jede
Standarddatei ins eingebundene Verzeichnis, *aber nur, wenn es dort
noch keine Datei dieses Namens gibt* (`cp -rn`). Der allererste Start
(leeres Verzeichnis) bekommt also den vollen Satz, ein späteres Image
mit einem neuen Standard-Screen fügt genau diese eine Datei hinzu, und
was der Sysop angepasst hat, wird nie angefasst. Die `configs/menus` und
`configs/screens` eines frischen Checkouts (für den Betrieb ohne Docker)
sind dieselben Dateien, von denen dieser Abgleich ausgeht -- wie die
Programme sie laden, beschreiben die Kommentare in `internal/bbs`.

`data/` (SQLite-Datenbank, SSH-Hostschlüssel, JWT-Schlüssel,
hochgeladene Dateien, installierte Doors -- siehe
`docs/adding-a-door.md` -- und die geänderten Texte unter `data/lang/`)
ist ebenfalls ein einfacher Bind-Mount (`./data`), kein benanntes
Volume. Es übersteht also `docker compose down` und ein Neuerstellen und
ist -- anders als ein benanntes Volume -- direkt vom Host aus
erreichbar: Ein Door installieren heisst einfach Dateien nach
`./data/doors/` kopieren, kein `docker cp`/`docker exec` nötig. Alle drei
Dienste schreiben gleichzeitig über den WAL-Modus in dieselbe
SQLite-Datenbank (siehe `internal/db`) -- dasselbe Prinzip wie ohne
Container, nichts Docker-Spezifisches.

Die Container laufen standardmässig als root (siehe `Dockerfile`), was
alles unter `./data` auf dem Host root gehören liesse.
`docker-compose.yml` setzt deshalb `user: "${PUID:-1000}:${PGID:-1000}"`
-- vor dem ersten Start auf den eigenen Host-Benutzer stellen, damit
Dateien der Container (und Doors, die man selbst hineinlegt) auf beiden
Seiten denselben Besitzer haben:

```sh
mkdir -p data
echo "PUID=$(id -u)" >> .env
echo "PGID=$(id -g)" >> .env
docker compose up -d --build
```

Gehört in `./data` schon etwas root, weil es früher ohne PUID/PGID lief
(oder von einem benannten Volume übernommen wurde), einmal die Besitzer
korrigieren: `sudo chown -R $(id -u):$(id -g) data`.

## Neustart aus dem Web-Admin

**Admin → System → Services** zeigt die drei Dienste (bbs, mailer, web)
mit Version und Laufzeit und startet sie neu. Dafür braucht es keinen
Docker-Socket: Jeder Dienst hält einen Herzschlag in der Datenbank,
achtet dort auf eine Neustart-Anforderung und beendet sich dann selbst
-- `restart: unless-stopped` der Container startet ihn innert Sekunden
neu. Der Mailer beendet vorher seine Runde und laufende eingehende
Sitzungen; die BBS kann warten, bis niemand online ist („Restart when
idle“).

Einstellungen, die ein Dienst nur beim Start liest, markieren ihn beim
Speichern (BinkP-Einstellungen und Netzwerke → mailer; Telnet/SSH, das
Level neuer Benutzer, Menüs und `welcome.ans` → bbs; die FTN-Adressen →
alle drei), und ein Banner im Admin bietet den Neustart an. Alles andere
-- Doors, Areas, andere Screens -- gilt sofort.

Ohne Docker (Programme von Hand gestartet) muss man einen so beendeten
Dienst selbst wieder starten.

## Image-Grösse und DOS-Doors

Das Laufzeit-Image ist `debian:trixie-slim`, nicht Alpine --
klassische DOS-Doors (`kind: dosbox` in `configs/bbs.yaml`, siehe
`docs/adding-a-door.md`) brauchen ein echtes `dosbox-x`-Paket mit seinen
Bibliotheken (SDL2, ein Audio-Stack für die FluidSynth-MIDI-Emulation
usw.), und das macht den grössten Teil des Images aus (~550 MB).
`dosbox-x` gibt es für Debian 12 (bookworm) gar nicht, deshalb gerade
trixie. Die Doors selbst liegen zur Laufzeit unter `data/doors/` (das
Verzeichnis oben), nicht im Image -- ein frischer Container hat keine
Doors, bis der Sysop eines dort installiert.

Door-Sitzungen laufen in DOSBox-X ganz ohne Bildschirm
(`SDL_VIDEODRIVER=dummy`, im Dockerfile gesetzt) -- kein X-Server und
kein virtueller Framebuffer im Container nötig; warum das für den hier
verwendeten `-socket`/`inhsocket:1`-Weg zuverlässig klappt, bei den
TCP-basierten Nullmodem-Modi von DOSBox-X aber nicht, steht in
`docs/adding-a-door.md`.

## Multi-Arch-Builds

Die Go-Build-Stufe kompiliert über `TARGETOS`/`TARGETARCH` (setzt
BuildKit selbst) mit `CGO_ENABLED=0` quer -- jede Abhängigkeit, auch
`modernc.org/sqlite`, ist reines Go, es braucht also keinen
C-Compiler und kein Basis-Image pro Architektur. `dosbox-x` gibt es in
Debians Paketquellen für `amd64` und `arm64`. Beide in einem Durchgang
mit buildx:

```sh
docker buildx build --platform linux/amd64,linux/arm64 --build-arg COMMIT=$(git rev-parse --short HEAD) -t nullmodem-bbs:latest .
```

`--build-arg COMMIT=…` schreibt den Commit in die Programme; er steht
neben der Version (Willkommensbildschirm, `?`-Befehl, Admin-Dashboard,
Services, Seitenfuss), zusammen mit dem Build-Zeitpunkt. Ohne ihn steht
nur der Zeitpunkt da.

## Ohne Compose

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

Für die anderen beiden Dienste in der letzten Zeile `./bin/bbs` durch
`./bin/mailer` oder `./bin/web` ersetzen (mit deren Ports; `web` braucht
zusätzlich `-v "$PWD/configs/web.yaml:/app/configs/web.yaml"`). Der
`mailer` zeigt weder BBS noch Designer und braucht `configs/menus` und
`configs/screens` deshalb nicht.

## Versionierte Image-Tags

Jeder Release-Tag (`vX.Y.Z`) auf GitHub baut das Image für `amd64` und
`arm64` und veröffentlicht es in der GitHub Container Registry, mit
diesem Tag und `latest`:

```sh
docker pull ghcr.io/midrei/nullmodem-bbs:vX.Y.Z
```

Statt selbst zu bauen, kann man das Image in den `image:`-Zeilen der
`docker-compose.yml` verwenden. Eine Installation besser auf einen
Versions-Tag festlegen als auf `latest` und bewusst mit
`docker compose pull && docker compose up -d` weitergehen, sobald es
einen neuen gibt -- zurück geht es genauso mit dem vorherigen Tag.

## Fremdsoftware

Das Image enthält Synchronets `sexyz` für Zmodem-Übertragungen, aus dem
Quellcode gebaut und unter der GNU GPL (v2 oder später) -- Lizenztexte,
Hinweise und der genaue Quellcode liegen im Image unter
`/usr/local/share/doc/sexyz/`. Siehe [third-party.md](third-party.md)
(englisch).
