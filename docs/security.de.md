# Sicherheit

[English](security.md) · **Deutsch**

Admin → Users → Security.

## Zwei-Faktor-Login (TOTP)

Unter „Your account: two-factor login“ einrichten: QR-Code mit einer
Authenticator-App scannen (Aegis, Google Authenticator, 1Password …), den
angezeigten Code eingeben, die 8 Recovery-Codes sicher aufbewahren (jeder
gilt einmal anstelle eines Codes).

Danach fragen nach dem Code:

- die Anmeldung im Web-Admin,
- das Sysop-Menü über Telnet/SSH (einmal pro Anruf).

Nicht betroffen (geben nur Benutzerrechte): Portal, Reader-App, QWK-Clients
wie NullModem Reader, der normale Telnet-Login.

„Require two-factor login for the admin and the Telnet sysop menu“ sperrt
Sysop-Konten ohne Zwei-Faktor aus Admin und Sysop-Menü aus. Ein anderer Sysop
kann einem Konto unter Users „Reset 2FA“ den zweiten Faktor wegnehmen (Handy
weg) — danach neu einrichten.

### Handy und Recovery-Codes weg, kein zweiter Sysop

Direkt auf dem Server, im Deploy-Verzeichnis (Beispiel-Handle `SwissMaik`):

```sh
python3 -c "import sqlite3; c=sqlite3.connect('data/nullmodem.sqlite'); \
c.execute(\"UPDATE users SET totp_secret='', totp_pending='', totp_last=0 WHERE username='SwissMaik'\"); \
c.execute(\"DELETE FROM totp_recovery WHERE user_id=(SELECT id FROM users WHERE username='SwissMaik')\"); c.commit()"
```

Wer Shell-Zugang zum Server hat, braucht keinen zweiten Faktor — der schützt
gegen gestohlene Passwörter, nicht gegen den Server selbst.

## Passwort vergessen

Users → „Password…“ setzt ein neues Passwort für ein Konto; dem Anrufer auf
einem anderen Weg mitteilen.

## Sperren und Listen

Fehl-Logins pro Adresse (Telnet, SSH, Portal, Admin, falsche Zwei-Faktor-Codes)
sperren die Adresse vorübergehend; Allow-/Blocklisten mit IP oder Bereich.
Hinter Caddy und Dockers Port-Mapping zählt die weitergereichte Adresse
(`X-Forwarded-For` von privaten Adressen).

## Warnungen

Alle 5 Minuten prüft der Web-Dienst: läuft BBS/Mailer/Door-Hintergrundprogramm,
gab es mit jedem Uplink in den letzten 48 h eine erfolgreiche Sitzung, ist das
Backup jünger als 26 h, ist genug Platz frei, hängt Netmail seit über zwei
Tagen, holt und sendet das E-Mail-Gateway (wenn an). Neue Probleme und deren Behebung kommen als Push aufs Handy (Reader-App
mit Benachrichtigungen) und stehen auf dem Dashboard.
