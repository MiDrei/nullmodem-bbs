# Sysop-Handbuch

Wie man eine NullModem BBS aufsetzt und im Alltag betreibt. Die Details stehen
in den verlinkten Einzeldokumenten; hier steht, was wohin gehört und in welcher
Reihenfolge man es angeht.

- [1. Aufbau](#1-aufbau)
- [2. Installation](#2-installation)
- [3. Die ersten Schritte](#3-die-ersten-schritte)
- [4. FTN-Netzwerke](#4-ftn-netzwerke)
- [5. Areas](#5-areas)
- [6. Doors](#6-doors)
- [7. Benutzer](#7-benutzer)
- [8. Für die Anrufer](#8-für-die-anrufer)
- [9. Im Alltag](#9-im-alltag)
- [10. Updates](#10-updates)
- [11. Fehlersuche](#11-fehlersuche)
- [12. Wo was liegt](#12-wo-was-liegt)

## 1. Aufbau

Drei Dienste aus demselben Docker-Image, verbunden über die gemeinsame
SQLite-Datenbank und `configs/bbs.yaml`:

| Dienst | Aufgabe | Port |
|---|---|---|
| `bbs` | Telnet, SSH, Doors, Chat | 2323, 2222 |
| `mailer` | BinkP, Tosser, Areafix/Filefix, Nodelisten, nächtliche Maintenance | 24554 |
| `web` | Portal, Reader-App, Web-Terminal, Administration, Backups, Warnungen | 8090 |

Davor gehört ein Reverse Proxy mit TLS (z. B. Caddy) für Port 8090. Die BBS
liest die echte Adresse der Besucher aus `X-Forwarded-For` — auch hinter
Dockers Port-Mapping.

## 2. Installation

Siehe [docker.md](docker.md). Kurz:

```sh
cp configs/bbs.yaml.example configs/bbs.yaml
docker compose up -d
```

In `.env` neben der `docker-compose.yml`:

```
PUID=1000          # Benutzer, dem data/ gehört
PGID=1000
TZ=Europe/Zurich   # Zeitzone: Logs, Backup-Namen, nächtliche Läufe
```

In `configs/web.yaml` für das Web-Terminal im Docker-Betrieb:
`terminal_addr: "bbs:2323"`.

## 3. Die ersten Schritte

1. **Sysop-Konto:** Per Telnet (oder `/terminal` im Browser) als Erster
   registrieren — das erste Konto wird automatisch Sysop (SL 255).
2. **Admin:** `https://deine-bbs/admin`, mit demselben Konto.
3. **Zwei-Faktor-Login einschalten:** Users → Security, siehe
   [security.md](security.md). Recovery-Codes sicher ablegen.
4. **Eigene Adresse auf die Allow-Liste** (Security), wenn du eine feste IP
   hast — dann sperrst du dich nie selbst aus.
5. **Grunddaten:** System → Settings (Name, Sysop, Ort — erscheint im
   BinkP-Handshake und auf den Bildschirmen).
6. **Bildschirme:** Content → Screens → Begrüßung, Hauptmenü usw. im ANSI-Designer
   anpassen. Rahmen mit `{FILL:x}` bauen, damit sie bei jeder Breite schließen.
7. **Benachrichtigungen:** Die Reader-App (`/reader`) aufs Handy legen und unter
   ⚙ Benachrichtigungen einschalten — dann kommen Warnungen, neue Benutzer und
   Pages aufs Handy.
8. **Backup prüfen:** System → Backups → „Back up now“, und für eine Kopie
   außerhalb des Servers sorgen ([backup.md](backup.md)).

## 4. FTN-Netzwerke

FTN → Networks & Addresses:

- **Netzwerk** anlegen (Name und Domain, z. B. `fsxNet` / `fsxnet`).
- **Eigene Adresse** pro Netzwerk (z. B. `21:3/194@fsxnet`).

FTN → Uplinks:

- **Uplink** (Hub) mit Adresse, Host, Session- und Packet-Passwort, Areafix-
  und Filefix-Passwort. „Crash only“, wenn man nur bei Post anrufen will;
  „Hold“, wenn der Hub abholt.
- **Areas abonnieren:** FTN → Areafix / Filefix — Liste beim Hub anfordern,
  ankreuzen, senden. Neue Echos aus eingehender Post landen erst unter
  „Pending areas“ und werden sichtbar, sobald du sie freigibst.
- **Points / eigene Reader-App:** [points.md](points.md).
- **Nodelisten:** kommen von selbst mit den File-Echos (`FSX_NODE` …) und
  werden alle 10 Minuten übernommen; Status unter FTN → Nodelists.

Was zu tun ist, wenn ein Hub nicht antwortet, steht unter
[Fehlersuche](#11-fehlersuche).

## 5. Areas

- **Message Areas:** Name, Beschreibung, Netzwerk, Security Level fürs Lesen
  und Schreiben, eigene Aufbewahrung (Tage / Anzahl). Daten-Areas wie
  `FSX_DAT` (InterBBS-Daten) als „hidden“ markieren (Content → Message Areas) — dann sehen Anrufer sie
  nicht.
- **File Areas:** analog, mit Download-/Upload-SL. TIC-Dateien mit „Replaces“
  ersetzen ältere Versionen automatisch.
- **Aufräumen:** System → Maintenance — Grenzen einstellen, „Preview“ zeigt,
  was weg würde, nachts läuft es von selbst.

## 6. Doors

Content → Doors. Fertige Vorlagen (MRC Chat, Usurper, Immortal Barons …) lassen
sich mit einem Klick installieren. Eigene Doors: [adding-a-door.md](adding-a-door.md).

| Art | Wofür |
|---|---|
| DOS (DOSBox-X) | klassische DOS-Doors, mit FOSSIL, Drop-Datei nach Wahl |
| Native Linux | Linux-Doors über DOOR32.SYS oder stdio |
| Remote (RLogin) | Door-Netzwerke wie DoorParty oder eine andere BBS |

Hintergrundprogramme (z. B. die MRC-Bridge) laufen als eigener Dienst und
erscheinen unter System → Services.

## 7. Benutzer

- **Neue Konten** warten auf deine Freischaltung (Users → „Awaiting approval“
  → Approve / Turn down). Bis dahin lesen sie und dürfen dir Netmail
  schreiben. Nie freigeschaltete Konten löscht die Maintenance nach 30 Tagen.
- **Security Levels:** Users (pro Konto) und SL Matrix (wer was darf).
- **Passwort vergessen:** Users → „Password…“ setzt ein neues.
- **Zweiter Faktor verloren:** Ein anderer Sysop setzt ihn mit „Reset 2FA“
  zurück; sonst siehe [security.md](security.md).
- **Gesperrte Adressen** und Fehl-Logins: Users → Security.

## 8. Für die Anrufer

**Telnet / SSH / Web-Terminal (`/terminal`):**

| Taste | |
|---|---|
| R / T | neue Nachrichten lesen / Nachrichten an mich |
| M / F | Message-Areas (dort S = Nachrichten suchen) / File-Areas (N = neue Dateien, S = Suche) |
| N / I | Netmail / Nodeliste |
| C / P | Chat (Teleconference) / Sysop rufen |
| L / V / B | One-Liner / Abstimmungen / BBS-Liste |
| W / D | Wer ist online (mit Node-Nachricht) / Doors |
| O / U / K | QWK holen / QWK-Antworten hochladen / Area-Auswahl |
| Y / ? | Profil (u. a. Zeileneditor statt Vollbild) / Version |

Nach dem Login: InterBBS Last Callers, One-Liner, Übersicht über Neues.

**Web:** Portal (`/`) mit allem aus Telnet (Suche über das Feld bei den Message Areas), Reader-App (`/reader`) fürs Handy
mit Offline-Lesen und Push, QWK-Reader wie NullModem Reader.

## 9. Im Alltag

- **Dashboard:** „Needs attention“ zeigt Probleme (Dienst steht, Uplink
  unerreichbar, Backup überfällig, Platte voll, Netmail hängt), wartende
  Benutzer, gesperrte Adressen und wer dich gerade ruft. Probleme kommen auch
  als Push.
- **Chat & One-Liner:** Community → Chat & One-liners — dort antwortest du, wenn
  jemand pagt, und räumst die One-Liner-Wand auf.
- **Abstimmungen / BBS-Liste:** Community → Polls & BBS List.
- **Logs:** System → Logs — „All“ mit „Warnings & errors“ als schneller
  Überblick; „BinkP sessions“ zeigt jede Sitzung samt Mitschnitt.
- **Nachts automatisch:** 03:00 Backup, 04:00 Maintenance (Serverzeit).

## 10. Updates

Neue Version: Image-Tag in der `docker-compose.yml` anpassen, dann

```sh
docker compose pull && docker compose up -d
```

Menüs und Bildschirme unter `configs/` bleiben dabei unangetastet; neue
Standard-Bildschirme kommen dazu, ohne angepasste zu überschreiben. Neue
Menüpunkte im Hauptmenü muss man darum selbst in `configs/menus/main.yaml`
und `configs/screens/main.ans` übernehmen (oder die Dateien aus dem Image
nehmen, wenn man sie nie angepasst hat). Vor größeren Updates:
„Back up now“.

## 11. Fehlersuche

| Symptom | Wo schauen | Häufige Ursache |
|---|---|---|
| Hub antwortet nicht | Logs → BinkP sessions → Mitschnitt | falsches Passwort, Hub down, Firewall |
| `M_BSY … busy` | Mitschnitt | der Hub glaubt, es läuft schon eine Sitzung (alte Sperrdatei bei ihm) — Hub-Sysop fragen |
| Post kommt nicht an | FTN → Packet Analyzer, Undeliverable Netmail | Area nicht abonniert / nicht freigegeben, falsches Packet-Passwort |
| Anrufer ausgesperrt | Users → Security | zu viele Fehl-Logins; Unlock |
| Door startet nicht | Logs → System (Door-Filter) | Verzeichnis leer, falsche Drop-Datei, Lock-Datei |
| Zeiten falsch | `.env` → `TZ` | Container ohne Zeitzone (UTC) |

Wer Shell-Zugang hat: `docker compose logs -f mailer` (bzw. `bbs`, `web`).

## 12. Wo was liegt

| Pfad | Inhalt |
|---|---|
| `configs/bbs.yaml` | Konfiguration (inkl. Passwörter) |
| `configs/web.yaml` | Web-Dienst |
| `configs/menus/`, `configs/screens/` | Menüs und ANSI-Bildschirme |
| `data/nullmodem.sqlite` | die Datenbank |
| `data/files/`, `data/doors/` | Dateien der File-Areas, installierte Doors |
| `data/backups/` | nächtliche Backups ([backup.md](backup.md)) |
| `data/binkp-sessions/`, `data/inbound-archive/` | BinkP-Mitschnitte, empfangene Pakete (ein paar Tage) |
| `data/jwt_secret`, `data/ssh_host_key`, `data/vapid.json` | Schlüssel — nicht weitergeben |
