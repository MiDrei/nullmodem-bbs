# Security

**English** · [Deutsch](security.de.md)

Admin → Users → Security.

## Two-factor login (TOTP)

Set it up under "Your account: two-factor login": scan the QR code with an
authenticator app (Aegis, Google Authenticator, 1Password …), enter the code
shown, keep the 8 recovery codes somewhere safe (each works once in place of
a code).

From then on these ask for the code:

- logging in to the web admin,
- the sysop menu over Telnet/SSH (once per call).

Not affected (they only grant user rights): portal, reader app, QWK clients
like NullModem Reader, the normal Telnet login.

"Require two-factor login for the admin and the Telnet sysop menu" locks
sysop accounts without a second factor out of the admin and the sysop menu.
Another sysop can take an account's second factor away under Users with
"Reset 2FA" (phone gone) — set it up again afterwards.

### Phone and recovery codes gone, no second sysop

Directly on the server, in the deploy directory (example handle `SwissMaik`):

```sh
python3 -c "import sqlite3; c=sqlite3.connect('data/nullmodem.sqlite'); \
c.execute(\"UPDATE users SET totp_secret='', totp_pending='', totp_last=0 WHERE username='SwissMaik'\"); \
c.execute(\"DELETE FROM totp_recovery WHERE user_id=(SELECT id FROM users WHERE username='SwissMaik')\"); c.commit()"
```

Whoever has shell access to the server doesn't need a second factor — it
protects against stolen passwords, not against the server itself.

## Forgotten password

Users → "Password…" sets a new password for an account; tell the caller by
some other way.

## Lockouts and lists

Failed logins per address (Telnet, SSH, portal, admin, wrong two-factor
codes) lock the address out for a while; allow/block lists with an IP or a
range. Behind Caddy and Docker's port mapping the forwarded address counts
(`X-Forwarded-For` from private addresses).

## Idle callers

A Telnet/SSH caller who doesn't type for 3 minutes at the login, or for the
"Hang up when idle" minutes once logged in (default 30, 0: never), is hung up
— otherwise a silent connection would hold its node for good. Time in a door,
a file transfer or an RLogin door doesn't count.

## Warnings

Every 5 minutes the web service checks: are BBS, mailer and door background
programs running, was there a successful session with every uplink in the
last 48 h, is the backup younger than 26 h, is there enough free space, has
netmail been stuck for more than two days, does the email gateway (when
on) fetch and send. New problems and their fixes come
as push notifications to your phone (reader app with notifications) and show
on the dashboard.
