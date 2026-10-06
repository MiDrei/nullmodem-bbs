# Standard-Screens

`space.py` erzeugt die Standard-Screens im Weltraum-Stil in
`configs/screens`: das Hauptmenü, seine Untermenüs (Nachrichten, Dateien,
Treffpunkt), das Sysop-Menü, den Abmelde-Screen sowie die Listen und
Ansichten für Nachrichtenbereiche, Nachrichten, Netmail, Dateibereiche und
Dateien, die Doors-Liste, die Köpfe der Treffpunkt-Funktionen, des Profils und der Übersicht nach
dem Login und den
Begrüssungs-Screen — jeweils als
`name.ans` (englischer Text) und `name.de.ans` (`{T:key}`-Platzhalter,
für Deutsch und Du gefüllt); Teile ohne eigenen Text (Listenzeilen) gibt
es nur als `name.ans`.

```sh
python3 scripts/screens/space.py            # schreibt configs/screens
python3 scripts/screens/space.py /tmp/out/  # oder anderswohin
```

`preview` zeichnet einen Screen so, wie ihn ein Terminal mit 80 Spalten
zeigt, als PNG:

```sh
go run ./scripts/screens/preview -lang de-du -sl 255 configs/screens/main.de.ans main.png
```

Mit `-sl` unter 200 zeigt es den Screen wie für einen Anrufer ohne die
Sysop-Zeilen.

Regeln für die Grafik:

- Höchstens 79 Spalten: Das BBS gibt Screens eine Spalte schmaler aus als
  das Terminal, weil viele Clients eine Zeile umbrechen, die die letzte
  Spalte füllt. `TestMenuScreensFit` prüft das.
- Rahmen und Texte, deren Breite je nach Sprache wechselt, mit `{FILL:x}`.
- `{SYSOP_ONLY}` in einer Zeile zeigt sie erst ab SL 200.
