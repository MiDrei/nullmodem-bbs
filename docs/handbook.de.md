# Sysop-Handbuch

[English](handbook.md) · **Deutsch**

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

Siehe [docker.md](docker.de.md). Kurz:

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
   [security.md](security.de.md). Recovery-Codes sicher ablegen.
4. **Eigene Adresse auf die Allow-Liste** (Security), wenn du eine feste IP
   hast — dann sperrst du dich nie selbst aus.
5. **Grunddaten:** System → Settings (Name, Sysop, Ort — erscheint im
   BinkP-Handshake und auf den Bildschirmen).
6. **Bildschirme:** Content → Screens → Begrüssung, Hauptmenü usw. im ANSI-Designer
   anpassen. Rahmen mit `{FILL:x}` bauen, damit sie bei jeder Breite schliessen. Eine Zeile mit
   `{SYSOP_ONLY}` sieht nur der Sysop (ab SL 200), für alle anderen fällt sie weg — so beim
   Sysop-Eintrag im Standard-Hauptmenü. Gespeichert gilt
   sofort, auch für den Begrüssungsbildschirm.
   **Menüs:** Content → Menus — Punkte (Taste, Text, was er tut, ab welchem
   SL) ändern, umsortieren, neue Menüs anlegen und verknüpfen. Die Vorschau
   zeigt das Menü wie ein Anrufer oder der Sysop und warnt, wenn Bildschirm und
   Punkte nicht zusammenpassen (ein Punkt fehlt auf dem Bildschirm, oder der
   Bildschirm zeigt eine Taste, die nichts tut). Gespeichert gilt es beim
   nächsten Menü der Anrufer, ohne Neustart.
7. **Benachrichtigungen:** Die Reader-App (`/reader`) aufs Handy legen und unter
   ⚙ Benachrichtigungen einschalten — dann kommen Warnungen, neue Benutzer und
   Pages aufs Handy.
8. **Backup prüfen:** System → Backups → „Back up now“, und die
   verschlüsselte Kopie ausser Haus einrichten (S3, Swift, SFTP oder WebDAV,
   [backup.md](backup.de.md)).

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
  „Hub fragen, was abonniert ist“ schickt `%QUERY`: Der Hub antwortet mit dem,
  was er uns tatsächlich verlinkt hat, die Haken richten sich danach,
  Abweichungen von unserer Aufzeichnung sind markiert, und „Stand des Hubs
  übernehmen“ übernimmt sein Wort in unsere Aufzeichnung (es wird nichts
  verschickt). Areafix (Echomail) und Filefix (File-Echos) antworten von
  derselben Adresse; jede Antwort zählt nur für den Robot, von dem sie
  kommt, und der Verlauf unter der Liste zeigt jede Anfrage und Antwort,
  Ablehnungen eingeschlossen (Passwörter ausgeblendet). Weitere Befehle:
  `%LINKED` (bei manchen Robots heisst `%QUERY` so), `%UNLINKED`, `%HELP`,
  `%PAUSE`, `%RESUME`. Ein Hub, der weder `%QUERY` noch `%LINKED` kennt
  (Clearing Houz, der Hub von fsxNet), markiert Abonniertes in seiner
  `%LIST` mit `*` -- danach richten sich dann die Haken.
- **Points / eigene Reader-App:** [points.md](points.de.md).
- **Nodelisten:** kommen von selbst mit den File-Echos (`FSX_NODE` …) und
  werden alle 10 Minuten übernommen; Status unter FTN → Nodelists.

Was zu tun ist, wenn ein Hub nicht antwortet, steht unter
[Fehlersuche](#11-fehlersuche).

## 5. Areas

- **Message Areas:** Name, Beschreibung, Netzwerk, Security Level fürs Lesen
  und Schreiben, eigene Aufbewahrung (Tage / Anzahl). Daten-Areas wie
  `FSX_DAT` (InterBBS-Daten) als „hidden“ markieren (Content → Message Areas) — dann sehen Anrufer sie
  nicht.
- **File Areas:** analog, mit Download-/Upload-SL. Mit „Public“ darf jeder ihre
  Dateien ohne Login laden: jede Datei hat eine Seite zum Teilen
  (`/share/f/<id>`, mit Link-Vorschau), das Portal zeigt dafür „Copy share
  link“, und die Startseite listet die neuesten. TIC-Dateien mit „Replaces“
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

**Tägliche Wartung:** Viele Doors (TradeWars, BRE, Usurper …) wollen einmal am
Tag ein Wartungsprogramm laufen sehen (neue Züge, Tagesereignisse). Im Door unter
„Daily maintenance“ den Befehl eintragen (bei DOS-Doors die DOS-Befehle, z. B.
`USURPER /MAINT`), dazu die Uhrzeit (Standard 00:05). Die BBS führt ihn ohne
Anrufer aus — nie, während jemand spielt, dann eben etwas später. Ergebnis und
Ausgabe stehen in der Door-Liste, „Run maintenance“ startet ihn sofort; ein
Fehlschlag erscheint unter „Needs attention“.

**Bulletins (Bestenlisten, News):** Viele Doors schreiben ihre Scoreboards
und News in Dateien. Beim Door unter „Bulletins“ Titel und Datei (relativ zum
Door-Verzeichnis) eintragen, „public“ zeigt sie auch auf der Startseite.
Anrufer lesen sie im Doors-Menü (B) und im Portal unter Community → Door
scores. Für Immortal Barons und Usurper Reborn kennt die BBS die Dateien:
„Use the template's …“ trägt sie ein — bei Immortal Barons schaltet das auch
`BulletinDir` in `data/bbs.cfg` ein und setzt die tägliche Wartung
(`immortal-barons -maint`), die sie jeden Tag neu schreibt.

**Updates:** Für MRC Chat, Immortal Barons und Usurper Reborn schaut die BBS
zweimal täglich nach neuen Versionen („Check for updates“ fragt sofort). Eine
neue erscheint beim Door mit ihren Release-Notes, unter „Needs attention“ und
als Push. „Update to …“ ersetzt nur die Dateien, die die Version mitbringt —
Spielstände, Einstellungen und Logs bleiben; die ersetzten Dateien liegen in
`.backup-<door>` neben dem Door-Verzeichnis (nur die vom letzten Update). Wer
gerade im Door spielt, behält die alte Version bis zum Verlassen; ein
Hintergrundprogramm startet von selbst neu. Lies vorher die Release-Notes:
manche Versionen verlangen etwas, z. B. dass alle Boards einer
Inter-BBS-Liga gemeinsam aktualisieren. Neu aus einer Vorlage installierte
Doors bekommen die neueste Version.

Hintergrundprogramme (z. B. die MRC-Bridge) laufen als eigener Dienst und
erscheinen unter System → Services.

## 7. Benutzer

- **Neue Konten** warten auf deine Freischaltung (Users → „Awaiting approval“
  → Approve / Turn down). Bis dahin lesen sie und dürfen dir Netmail
  schreiben. Nie freigeschaltete Konten löscht die Maintenance nach 30 Tagen.
- **Security Levels:** pro Konto unter Users. Unter Users → Security Levels
  gibst du den Levels, die du verwendest, einen Namen (z. B. 10 Neuer
  Benutzer, 20 Regulärer Benutzer, 30 Regulär und E-Mail, 255 Sysop); jedes
  Level-Feld im Admin bietet sie dann mit Namen an, und das Telnet-Sysop-Menü
  zeigt sie beim Setzen eines Levels. Ohne eigene Namen erscheinen die Levels
  der BBS (wartet auf Freischaltung, neuer Benutzer, Sysop).
- **Passwort vergessen:** Users → „Password…“ setzt ein neues.
- **Zweiter Faktor verloren:** Ein anderer Sysop setzt ihn mit „Reset 2FA“
  zurück; sonst siehe [security.md](security.de.md).
- **Gesperrte Adressen** und Fehl-Logins: Users → Security.

## 8. Für die Anrufer

**Telnet / SSH / Web-Terminal (`/terminal`):**

Das Hauptmenü ist kurz; der Rest steckt in drei Untermenüs (Q geht zurück):

| Hauptmenü | |
|---|---|
| R | neue Nachrichten lesen |
| N | News vom Sysop |
| M » | **Nachrichten:** R neue Nachrichten · T Nachrichten an mich · A Nachrichtenbereiche (dort S = suchen) · N Netmail (und E-Mail, wenn das Gateway an ist) · I Nodeliste · K meine Bereiche · O QWK holen · U QWK-Antworten hochladen |
| F » | **Dateien:** A Dateibereiche · N neue Dateien · S Dateien suchen |
| T » | **Treffpunkt:** C Chat (Telekonferenz; dort `/rooms`, `/join name`) · P Sysop rufen · L One-Liner · W wer ist online (mit Node-Nachricht) · Z letzte Anrufer · V Abstimmungen · B BBS-Liste |
| D / P | Doors / Profil (u. a. Zeileneditor statt Vollbild) |
| S / ? / Q | Sysop-Menü / Version / abmelden |

**Meine Areas** (K, im Portal „My areas / All areas“ mit ✓ pro Area, in der
Reader-App unter „All“): Was dort drin ist, nehmen New-Scan, QWK-Pakete und die
Reader-App (Ungelesen-Liste, Offline-Vorabladen) mit. Alles ist drin, bis man
eine Area herausnimmt — neue Areas kommen also automatisch dazu. Nachrichten
*an mich* (T) und Push-Meldungen dafür kommen aus allen Areas.

In der Nachrichtenliste einer Area schaltet `T` auf Threads um: eine Zeile
pro Thread (Anzahl Nachrichten, wer ihn begann, letzte Aktivität, NEW solange
etwas ungelesen ist), Enter liest den Thread in Antwort-Reihenfolge.

Im Nachrichten-Reader: `]` (oder `T`) springt zur nächsten Nachricht im
Thread, `[` zur vorherigen — Antworten in der Reihenfolge, wie sie aufeinander
antworten, über die ganze Area hinweg.

Nach dem Login: InterBBS Last Callers, One-Liner, Übersicht über Neues.

**Sprachen:** Die BBS spricht Englisch, Deutsch (Sie) und Deutsch (Du) — auf
Telnet/SSH wie im Portal, in der Reader-App, auf der Startseite und im Admin
(Sprachwahl oben rechts, das Kürzel neben dem Mond).
Auf Telnet/SSH wählt jeder Anrufer unter dem Begrüssungsbildschirm eine
Sprache, noch vor der Frage nach dem Handle (eine Taste, Enter behält die des
Boards) — Login und Registrierung laufen dann darin, und ein neues Konto
behält sie. Später ändert man sie im Profil (Telnet `Y`, dann `A`; im Portal
unter Profil, in der Reader-App in den Einstellungen) — es ist eine
Einstellung fürs Konto, überall gleich; bei einem bestehenden Konto gilt ab
dem Login seine Sprache. Für alle, die nie gewählt haben, gilt die Sprache
des Boards (Content → Languages, „The board's language“); im Web nimmt ein Besucher ohne
Konto die Sprache seines Browsers (Deutsch in der Form des Boards, Sie oder
Du) und kann sie oben auf der Seite wechseln. Auch Fehlermeldungen,
Push-Benachrichtigungen und der Begrüssungsbildschirm (`welcome.de.ans`) folgen
der Sprache.
Was einer Sprache fehlt, kommt auf Englisch; Deutsch (Du) nimmt zuerst von
Deutsch (Sie), was gleich lautet.

- **Texte ändern:** Content → Languages. Jeder Text lässt sich pro Sprache
  überschreiben; leer heisst „wie mitgeliefert“. `{NAME}` sind Platzhalter, die
  die BBS füllt — ein Text darf einen weglassen, aber keinen erfinden (der
  Editor zeigt, welche gehen). Gespeichert wird nur, was du geändert hast,
  in `data/lang/<sprache>.yaml`; es gilt sofort und übersteht Updates.
  Die Texte sind nach Bereichen (Telnet/SSH, Screens & Menüs, Web, Admin,
  Meldungen) und Gruppen geordnet; ein Text, der an mehreren Stellen
  vorkommt, gibt es nur einmal („Auch verwendet in: …“ sagt, wo sonst).
- **Screens:** Zu jedem Screen kann es eine Fassung pro Sprache geben:
  `main.de-du.ans`, dann `main.de.ans`, dann `main.ans`. Mitgeliefert sind
  deutsche Fassungen der Standard-Screens; sie holen ihre Texte mit
  `{T:schlüssel}` aus dem Katalog (`{T:col.subject:-40}` linksbündig auf 40
  Zeichen, `{T:col.total:5}` rechtsbündig) — eine Datei für Sie und Du, und
  die Beschriftungen ändert man im Spracheditor. `{T:…}` geht in jedem Screen,
  auch in eigenen. Den eigenen `welcome.ans` übersetzt man als
  `welcome.de.ans` im Designer.
- **Menüs:** Ein mitgelieferter Menüpunkt erscheint von selbst übersetzt. Eigene
  Beschriftungen bekommen ihre Übersetzung im Menü-Editor („Other
  languages…“); die Vorschau zeigt jede Sprache.

**Startseite (`/`):** öffentlich, ohne Login — Begrüssungsbildschirm, alle
Zugänge (Web-Terminal, Telnet/SSH, Portal, Reader-App, QWK), wer online ist,
letzte Anrufer, One-Liner, Doors und die FTN-Adressen für andere Sysops. Die
Adresse, die man weitergibt. Ein geteilter Link (Telegram, Discord, Mastodon …) zeigt als
Vorschau den Begrüssungsbildschirm (`/og-image.png`, aus `welcome.ans` gezeichnet).

**RSS-Feeds:** System → Settings → „Public RSS feeds“ einschalten, dann gibt es
für jede Area, die ein neuer Anrufer lesen darf, `/feeds/<tag>.xml` mit den
neuesten 30 Nachrichten (die Startseite listet sie, Feed-Reader finden sie
selbst). Areas mit höherem SL (Sysop, lokal Privates) bleiben draussen.

**Threads:** Jede Antwort weiss, worauf sie antwortet — aus dem REPLY-Kludge
der Echomail, bei Antworten hier direkt (Telnet, Portal, Reader-App, QWK).
Ältere Nachrichten ohne diese Angabe werden über den Betreff („Re: …“)
zugeordnet. Im Portal zeigt jede Area „All messages“ oder „Threads“, jede
Nachricht ihren Thread als Baum; ausgehende Antworten tragen ein REPLY, damit
andere Systeme sie ebenfalls einordnen.

**Chat im Web:** Das Portal (Chat) und die Reader-App (Chat in der Liste)
haben dieselben Räume wie die Teleconference — wer im Portal schreibt, ist für
Telnet-Anrufer da (`name (web)`), und gebrückte Räume reichen bis Discord und
Matrix.

**Web:** Portal (`/message-areas` …, Login unter `/login`) mit allem aus Telnet (Suche über das Feld bei den Message Areas), Reader-App (`/reader`) fürs Handy
mit Offline-Lesen und Push, QWK-Reader wie NullModem Reader.

## 9. Im Alltag

- **Dashboard:** „Needs attention“ zeigt Probleme (Dienst steht, Uplink
  unerreichbar, Backup überfällig, Platte voll, Netmail hängt), wartende
  Benutzer, gesperrte Adressen und wer dich gerade ruft. Probleme kommen auch
  als Push. Darunter pro Uplink, wann die letzte Sitzung geklappt hat, der
  letzte Fehler und die Sitzungen der letzten 24 Stunden (roter Punkt: letzte
  Sitzung fehlgeschlagen, gelb: seit zwei Tagen nichts durchgekommen, grau:
  wird nicht abgeholt); und das System — letztes Backup, Off-site-Kopie,
  Grösse der Datenbank, freier Speicher, Dienste und die letzten Warnungen
  und Fehler.
- **Chat & One-Liner:** Community → Chat & One-liners — dort antwortest du, wenn
  jemand pagt, und räumst die One-Liner-Wand auf.
- **Chat-Räume:** ebenda unter „Rooms“. Neben der Teleconference (`main`)
  beliebig viele, je mit Thema und Mindest-SL. Anrufer sehen sie in der
  Teleconference mit `/rooms` und wechseln mit `/join name`.
  Sysops kommen und gehen still (alle anderen werden gemeldet); „Say in the
  rooms when a sysop enters or leaves“ ändert das. **Leeren** in einem
  offenen Raum löscht alles, was darin gesagt wurde; sonst verschwinden die
  Zeilen nach 30 Tagen.
- **Discord-Brücke:** Ein Raum kann mit einem Discord-Kanal verbunden werden:
  Was in der BBS gesagt wird, erscheint dort unter dem Namen des Anrufers, was
  in Discord geschrieben wird, in der BBS als `name@discord`. Die BBS baut nur
  ausgehende Verbindungen auf, es braucht keinen offenen Port und keinen
  eigenen Server. Einrichten (ca. 10 Minuten):
  1. Eigener Discord-Server, falls noch keiner da ist: im Discord-Programm
     unten in der Serverliste **+** → „Create My Own“.
  2. [Developer Portal](https://discord.com/developers/applications) → **New
     Application** (der Name wird der Name des Bots).
  3. **Bot** → **Message Content Intent** einschalten → Save.
  4. **Bot** → **Reset Token** → Token kopieren, in der BBS unter Community →
     Chat & One-liners → Discord bridge einfügen → **Turn on**.
  5. Sobald dort „Connected“ steht: **Add it to your server** — der Link fragt
     die nötigen Rechte an (Kanäle sehen, schreiben, Verlauf lesen, Webhooks
     verwalten).
  6. Bei jedem Raum unter **Edit** den Kanal wählen.

  Ohne das Recht „Webhooks verwalten“ schreibt der Bot selbst (`**name**:
  text`). Ein- und Austritte meldet er, solange „Don't tell Discord …“ aus
  ist. Ist die Brücke eingeschaltet, aber länger als 15 Minuten getrennt,
  erscheint das unter „Needs attention“. Das Token steht in `bbs.yaml` und
  wird im Web nie wieder angezeigt.
- **Matrix-Brücke:** genauso, für Matrix-Räume (z. B. auf matrix.org): ein
  Konto für den Bot anlegen (etwa über Element), unter „Matrix bridge“
  Homeserver, Bot-Name und Passwort eintragen → „Log in and turn on“ (es wird
  nur das Zugriffstoken gespeichert). Mit dem eigenen Konto einen Raum **ohne
  Verschlüsselung** anlegen und den Bot einladen (oder den Raum öffentlich
  machen), dann beim BBS-Raum unter Edit den Matrix-Raum wählen oder seine
  Adresse `#raum:server` eintragen. In Matrix schreibt der Bot „name: text“,
  in der BBS erscheint `name@matrix`. Verschlüsselte Räume kann der Bot nicht
  lesen — das Admin warnt dann. Ein Raum kann gleichzeitig mit Discord und
  Matrix verbunden sein; was in Discord gesagt wird, kommt dann auch in Matrix
  an und umgekehrt.
- **E-Mail-Gateway:** Community → Email gateway. Jeder Anrufer ab dem
  eingestellten Level hat die Adresse handle@deine-domain (SwissMaik ist
  `swissmaik@bbs.example.com`, ein Leerzeichen wird zum Punkt) und schreibt
  E-Mail überall dort, wo er Netmail schreibt: eine E-Mail-Adresse als
  Empfänger. Wer auf eine Mail antwortet, antwortet per Mail — über Telnet,
  im Portal, in der Reader-App und in einem QWK-Reader. Einrichten:
  1. Beim Mail-Anbieter ein Postfach für die Domain mit **Catch-all** (alle
     Mail an irgendeine Adresse der Domain landet dort) und ein SMTP-Login,
     das als jede Adresse der Domain senden darf — meist dasselbe Konto.
  2. Domain, IMAP-Server (das Postfach) und SMTP-Server eintragen, **Test**,
     einschalten, speichern.

  Oder ohne Postfach: die BBS als eigener Mail-Server der Domain oder der
  Webhook eines Weiterleitungsdienstes -- siehe [email.de.md](email.de.md).

  Die BBS holt das Postfach jede Minute ab; ungelesene Mail wird zur Netmail
  an den Anrufer, an den sie geht (`name+irgendwas@` geht auch), und danach
  als gelesen markiert oder, wenn gewünscht, gelöscht. Mail an unbekannte
  Adressen, an Anrufer unter dem Level oder noch nicht freigeschaltete und
  Mail, die der Anbieter als Spam markiert hat, wird verworfen. Nur Text:
  HTML-Mail wird zu Text, Anhänge werden genannt, aber nicht zugestellt.
  Jede verschickte Mail beginnt mit einer Zeile, wer sie auf welcher BBS
  geschrieben hat (`email.sent_by` im Spracheditor, z. B. um die
  Web-Adresse der BBS zu ergänzen). Ein
  Tageslimit pro Anrufer verhindert, dass ein Konto zur Spamschleuder wird.
  Lehnt der Server des Empfängers eine Mail ab, kommt sie als Netmail mit
  dem Grund zum Schreiber zurück; kann das Gateway eine halbe Stunde lang
  nicht abholen oder senden, steht es unter „Needs attention“. Die Anrufer
  sehen ihre Adresse im Profil.
- **News:** Content → News. Eine Meldung hat Titel und Text auf Deutsch und
  auf Englisch (wer Deutsch liest, bekommt die deutsche, alle anderen die
  englische; fehlt eine Sprache, erscheint die andere) und auf Wunsch ein
  Datum, bis zu dem sie erscheint. Beim Login sieht ein Anrufer die Meldungen,
  die er noch nicht gesehen hat, im Hauptmenü unter N alle aktuellen; die
  neuesten drei stehen auf der Startseite und im Portal (Treffpunkt).
- **Abstimmungen / BBS-Liste:** Community → Polls & BBS List. Die BBS prüft
  stündlich, ob die Boards der Liste antworten (TCP-Verbindung, nichts wird
  gesendet) und zeigt „up/down“ bzw. „online/offline“; Adressen im eigenen
  oder einem privaten Netz werden nie angefragt.
- **Monatsrückblick:** Am 1. um 07:00 bekommt jeder Sysop eine Netmail mit dem
  Vormonat — Anrufe, Schreiber, Areas, Echomail pro Netzwerk, Doors, Downloads,
  BinkP-Sitzungen, Backup und was gerade nicht stimmt. Abschalten unter
  Settings → Monthly recap; System → Statistics → „Send a recap now“ schickt
  sofort einen.
- **Statistik:** System → Statistics — Anrufe pro Tag und Stunde, aktivste
  Anrufer, Schreiber und Areas, Echomail pro Netzwerk, Doors, Downloads,
  BinkP-Sitzungen, neue Konten (7 Tage bis 1 Jahr). Die letzten 30 Tage ohne
  den Sysop-Teil stehen auch auf der Startseite.
- **Logs:** System → Logs — „All“ mit „Warnings & errors“ als schneller
  Überblick; „BinkP sessions“ zeigt jede Sitzung samt Mitschnitt.
- **Nachts automatisch:** 00:05 Door-Wartung (pro Door einstellbar), 03:00
  Backup, 04:00 Maintenance (Serverzeit).

## 10. Updates

Neue Version: Image-Tag in der `docker-compose.yml` anpassen, dann

```sh
docker compose pull && docker compose up -d
```

Menüs und Bildschirme unter `configs/` bleiben dabei unangetastet; neue
Standard-Bildschirme kommen dazu, ohne angepasste zu überschreiben. Neue
Menüpunkte zeigt Content → Menus an („This version's stock main menu has,
and yours doesn't“) — mit **Add** übernehmen und speichern; die Vorschau
sagt dann, ob der Bildschirm (`main.ans`) den Punkt schon zeigt, sonst
„Edit screen“. Vor grösseren Updates: „Back up now“.

## 11. Fehlersuche

| Symptom | Wo schauen | Häufige Ursache |
|---|---|---|
| Hub antwortet nicht | Logs → BinkP sessions → Mitschnitt | falsches Passwort, Hub down, Firewall |
| `M_BSY … busy` | Mitschnitt | der Hub glaubt, es läuft schon eine Sitzung (alte Sperrdatei bei ihm) — Hub-Sysop fragen |
| Post kommt nicht an | FTN → Packet Analyzer, Undeliverable Netmail | Area nicht abonniert / nicht freigegeben, falsches Packet-Passwort |
| Anrufer ausgesperrt | Users → Security | zu viele Fehl-Logins; Unlock |
| Door startet nicht | Logs → System (Door-Filter) | Verzeichnis leer, falsche Drop-Datei, Lock-Datei |
| Zeiten falsch | `.env` → `TZ` | Container ohne Zeitzone (UTC) |
| Reste auf dem Bildschirm beim Blättern in Listen | Terminal des Anrufers | es kommt mit dem Aktualisieren an Ort und Stelle nicht zurecht (gesendet werden nur geänderte Zeilen); `NULLMODEM_FULL_REDRAW=1` in der Umgebung des `bbs`-Dienstes sendet wieder jeden Bildschirm ganz |

Wer Shell-Zugang hat: `docker compose logs -f mailer` (bzw. `bbs`, `web`).

## 12. Wo was liegt

| Pfad | Inhalt |
|---|---|
| `configs/bbs.yaml` | Konfiguration (inkl. Passwörter) |
| `configs/web.yaml` | Web-Dienst |
| `configs/menus/`, `configs/screens/` | Menüs und ANSI-Bildschirme (`name.de.ans` = deutsche Fassung) |
| `data/lang/` | deine geänderten Texte pro Sprache (Spracheditor) |
| `data/nullmodem.sqlite` | die Datenbank |
| `data/files/`, `data/doors/` | Dateien der File-Areas, installierte Doors |
| `data/backups/` | nächtliche Backups ([backup.md](backup.de.md)) |
| `data/binkp-sessions/`, `data/inbound-archive/` | BinkP-Mitschnitte, empfangene Pakete (ein paar Tage) |
| `data/jwt_secret`, `data/ssh_host_key`, `data/vapid.json` | Schlüssel — nicht weitergeben |
