# E-Mail: Mail direkt empfangen

[English](email.md) · **Deutsch**

Das E-Mail-Gateway (Community → Email gateway, siehe
[Handbuch](handbook.de.md)) holt die Mail der Domain normalerweise per
IMAP aus einem Catch-all-Postfach. Es kann die Mail auch selbst annehmen,
auf zwei Wegen -- mit oder ohne Postfach. Der Versand bleibt, wie er ist:
über den eingestellten SMTP-Server (Smarthost).

Mail an `handle@domain` erreicht den Anrufer so oder so als Netmail. Die
Domain ist die des Gateways (**Domain** im Admin); **Weitere Domains**
fügt weitere hinzu, für dieselben Anrufer. `postmaster@` und `abuse@`
jeder Domain gehen an den Sysop.

## 1. Die BBS als Mail-Server der Domain (MX)

1. **DNS:** ein MX-Eintrag der Domain, der auf einen Namen mit der
   öffentlichen Adresse dieses Rechners zeigt, z. B.

   ```
   bbs.example.com.   IN MX 10 bbs.example.com.
   bbs.example.com.   IN A     203.0.113.7
   ```

2. **Port 25:** TCP-Port 25 im Router auf den Rechner weiterleiten und
   beim Dienst `web` in der `docker-compose.yml` freigeben
   (`- "25:2525"`, steht dort schon als Kommentar). Im Container lauscht
   der Server auf 2525 (**Lauscht auf** im Admin).
   Viele Privatanschlüsse sperren eingehenden Port 25 -- kommt nie Mail
   an, stattdessen den Webhook nehmen (Abschnitt 2).
3. **Admin:** Email gateway → **Mail-Server der Domain sein (MX)**,
   speichern. Der Status-Kasten zeigt, ob er lauscht.

Was der Server an der Tür prüft, bevor etwas gespeichert wird:

- **Unbekannte Empfänger** (und Anrufer ohne E-Mail) werden abgewiesen,
  solange der Absender noch verbunden ist -- so geht kein Bounce an einen
  gefälschten Absender.
- **SPF:** Ein Server, den die Absender-Domain nicht erlaubt, wird
  abgewiesen; ein «Softfail» gilt als Spam (zugestellt nur mit **Spam
  zustellen**).
- **Blocklisten** (Vorgabe `zen.spamhaus.org`): Ein gelisteter Server
  wird vor der Begrüssung abgewiesen. Spamhaus beantwortet Anfragen über
  grosse öffentliche DNS-Server (Google, Cloudflare) nicht; dann entfällt
  die Prüfung und gilt nie als Treffer. Damit sie wirkt, braucht der
  Rechner einen eigenen Resolver oder den des Providers.
- **Greylisting:** Ein noch unbekanntes Trio aus Absender, Empfänger und
  Netz wird gebeten, es nochmals zu versuchen; echte Mail-Server tun das
  innert Minuten, das meiste Spam nicht. Bekannte Absender kommen danach
  sofort durch.
- **Limits:** 25 MB pro Mail (über 5 MB kommen Kopfzeilen und ein
  Hinweis an), 50 Empfänger, 3 Verbindungen pro Adresse.
- **STARTTLS** mit einem selbst erzeugten Zertifikat; Mail-Server, die
  an einen MX zustellen, verschlüsseln damit, ohne es zu prüfen.

Zum Testen von einem anderen Rechner aus:
`swaks --to handle@bbs.example.com --server bbs.example.com` (der erste
Versuch wird gegreylistet: nach 5 Minuten nochmals).

## 2. Ein Weiterleitungsdienst (Webhook)

Ein Dienst nimmt die Mail der Domain an und schickt jede per HTTPS an die
BBS -- ohne Port 25. Im Admin: **Webhook-Adresse erzeugen**, dann **Mail
von einem Weiterleitungsdienst annehmen**. Die Adresse ist
`https://deine-bbs/api/email/inbound?key=SCHLÜSSEL`; geheim halten
(**Neuer Schlüssel** ersetzt sie).

**Forward Email** (forwardemail.net, bezahlter Plan -- im Gratis-Plan
steht die Webhook-Adresse samt Schlüssel öffentlich im DNS):

1. Den MX der Domain auf `mx1.forwardemail.net` und
   `mx2.forwardemail.net` setzen (je Priorität 10) und die Domain dort
   bestätigen.
2. My Account → Domains → *deine Domain* → Aliases → ein Catch-all (`*`)
   mit Weiterleitung an
   `https://bbs.example.com/api/email/inbound?key=SCHLÜSSEL&attachments=false`
   (`attachments=false`: Die Anhänge stecken schon in der Rohmail, das
   halbiert die Anfrage).
3. My Account → Domains → Settings → *Webhook Signature Payload
   Verification Key*: im Admin unter **Signaturschlüssel** eintragen. Ab
   dann wird eine Anfrage ohne gültige `X-Webhook-Signature` von Forward
   Email abgewiesen.

Auch der Versand kann über Forward Email laufen: deren SMTP-Server
(`smtp.forwardemail.net`, Port 465 TLS) mit einem Alias der Domain und
dessen generiertem Passwort unter **Versand (SMTP)** -- eine ausgehende
Verbindung, zu Hause muss nichts geöffnet werden.

**Cloudflare Email Routing** (Domain bei Cloudflare): Email → Email
Routing → Routing rules → Catch-all → *Send to a Worker*, mit diesem
Worker und dem Schlüssel als Worker-Secret `NULLMODEM_SECRET`:

```js
export default {
  async email(message, env) {
    const res = await fetch(
      "https://bbs.example.com/api/email/inbound?to=" + encodeURIComponent(message.to),
      {
        method: "POST",
        headers: { Authorization: "Bearer " + env.NULLMODEM_SECRET, "Content-Type": "message/rfc822" },
        body: message.raw,
      },
    );
    if (!res.ok) message.setReject("BBS answered " + res.status);
  },
};
```

**Mailgun:** Receiving → Create route → Catch-all → *Forward* an
`https://bbs.example.com/api/email/inbound/mime?key=SCHLÜSSEL` (das
`mime` lässt Mailgun die Rohmail schicken).

**Alles andere**, das eine Mail per HTTP schicken kann: die Rohmail als
Body (`message/rfc822`), die Empfänger in `?to=` (durch Komma getrennt)
oder einem `X-Envelope-To`-Header, den Schlüssel als `?key=` oder
`Authorization: Bearer`. Antworten: 200 angenommen, 401 falscher
Schlüssel oder falsche Signatur, 404 Webhook aus, 503 später nochmals.
Eine Anfrage über 64 MB wird verworfen (mit 200 beantwortet und
protokolliert), damit sie nicht tagelang wiederholt wird.

## Das IMAP-Postfach

Ist einer der beiden Wege an, ist das IMAP-Postfach optional; ohne
IMAP-Server wird nicht mehr abgeholt. Während des Umstiegs kann beides
nebeneinander laufen.
