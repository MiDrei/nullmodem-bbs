# Email: receiving mail directly

**English** · [Deutsch](email.de.md)

The email gateway (Community → Email gateway, see the
[handbook](handbook.md)) normally fetches the domain's mail from a
catch-all mailbox over IMAP. It can also take the mail in itself, in two
ways -- with or without the mailbox. Sending stays as it is: through the
SMTP server you set (a smarthost).

Mail to `handle@domain` reaches the caller as netmail either way. The
domain is the gateway's (**Domain** in the admin); **Further domains**
adds more, for the same callers. `postmaster@` and `abuse@` of each
domain reach the sysop.

## 1. The BBS as the domain's mail server (MX)

1. **DNS:** an MX record for the domain pointing at a name with this
   machine's public address, e.g.

   ```
   bbs.example.com.   IN MX 10 bbs.example.com.
   bbs.example.com.   IN A     203.0.113.7
   ```

2. **Port 25:** forward TCP port 25 from your router to the machine, and
   publish it from the `web` service in `docker-compose.yml`
   (`- "25:2525"`, already there as a comment). Inside the container the
   server listens on 2525 (**Listens on** in the admin).
   Many home connections block incoming port 25 -- if mail never
   arrives, use the webhook (section 2) instead.
3. **Admin:** Email gateway → **Be the domain's mail server (MX)**, save.
   The status box says whether it's listening.

What the server does at the door, before anything is stored:

- **Unknown recipients** (and callers without email) are refused while
  the sender is still connected, so no bounce goes to a faked sender.
- **SPF:** a server the sender's domain doesn't allow is refused; a
  "soft fail" counts as spam (delivered only with **Deliver spam**).
- **Block lists** (default `zen.spamhaus.org`): a listed server is
  refused before the greeting. Spamhaus doesn't answer queries through
  big public resolvers (Google, Cloudflare); then the check is skipped,
  never mistaken for a listing. For it to work, the machine needs its
  own resolver or your provider's.
- **Greylisting:** a sender, recipient and network not seen before is
  asked to try again; real mail servers do so within minutes, most spam
  doesn't. Known senders get through at once from then on.
- **Limits:** 25 MB per mail (bigger than 5 MB arrives as its headers
  and a note), 50 recipients, 3 connections per address.
- **STARTTLS** with a certificate the server makes itself; mail servers
  delivering to an MX encrypt with it without checking it.

To test from another machine: `swaks --to handle@bbs.example.com --server bbs.example.com`
(the first try is greylisted: run it again after 5 minutes).

## 2. A forwarding service (webhook)

A service receives the domain's mail and posts each one to the BBS over
HTTPS -- no port 25 needed. In the admin: **Create the webhook
address**, then **Take mail from a forwarding service**. The address is
`https://your-bbs/api/email/inbound?key=SECRET`; keep it secret (**New
secret** replaces it).

**Cloudflare Email Routing** (domain on Cloudflare): Email → Email
Routing → Routing rules → catch-all → *Send to a Worker*, with this
worker and the secret as the worker's secret `NULLMODEM_SECRET`:

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

**Mailgun:** Receiving → Create route → catch-all → *Forward* to
`https://bbs.example.com/api/email/inbound/mime?key=SECRET` (the `mime`
makes Mailgun post the raw mail).

**Anything else** that can post a mail: the raw message as the body
(`message/rfc822`), the recipients in `?to=` (comma-separated) or an
`X-Envelope-To` header, the secret as `?key=` or
`Authorization: Bearer`. Answers: 204 taken, 401 wrong secret, 404 the
webhook is off, 503 try again later.

## The IMAP mailbox

With either way on, the IMAP mailbox is optional; leave its server empty
to stop fetching. Both can run side by side while you move over.
