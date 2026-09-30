# Points: your own reader app (FidoMail and the like)

A point is a reader system under this BBS's own node address:
`21:3/194.1` is point 1 of `21:3/194`. Offline reader apps such as
FidoMail on iOS work as points -- they call the BBS over BinkP, fetch
the areas they subscribed to and hand over what was written in them.

## Setting one up

In **Admin → BinkP → Uplinks → Nodes / Points**, add one entry per
network the reader should take part in:

| Field | Value |
| --- | --- |
| Address | the point address, e.g. `21:3/194.1` (fsxNet) or `954:700/14.1` (HobbyNet) |
| Host | a label, the **same** for all entries of this reader (e.g. `fidomail`); the reader calls in, it's never dialed |
| Session password | the reader's BinkP password -- the same on every entry |
| Areafix password | for the reader's Areafix requests (`+AREA`, `%LIST`) |
| Network | the network of that address |
| Downlink | on; **Hold** on too, since a reader can't be called |

The reader is configured the other way round: this BBS as its boss
node (host and BinkP port, e.g. `bbs.maik.ch:24554`), its point
addresses, the same password. Its areas are subscribed through
Areafix, or ticked for it in the web admin (area grants, which are
kept per host label -- hence one label for all its entries).

A point, unlike a node, only gets what's its own:

- netmail addressed to it, and never anyone else's -- this system's
  outgoing netmail and echomail still go to the hubs;
- the areas it subscribed to: from two weeks before the subscription
  on, each message once (tracked per point, since SEEN-BY can't name
  points).

## Post as a BBS user

**Post as BBS user** on a point makes it your own reader for your BBS
account: what you write in the reader appears as if you'd written it
on the BBS itself.

- Echomail is stored as that user's post in the area and goes to the
  hub with this system's address, MSGID, tearline and origin line --
  the reader's own kludges, tearline and origin are dropped. It isn't
  sent back to the reader.
- Netmail goes out as that user from this system's address in the
  destination's zone; netmail to someone on this BBS lands in their
  inbox, from you.
- Netmail to that user is also copied to the reader (the last two
  weeks when first set up), to its point address in the sender's zone.
  The original stays in the BBS inbox.
- The areas ticked for it in the web admin (area grants) are its
  subscriptions: no Areafix request needed. Unticking one ends it.
  Areafix still works as well.

A resent packet (the reader didn't see our acknowledgement) isn't
posted twice: the reader's MSGID is kept to recognise it. The user must
exist; if it's renamed or deleted, the reader's sessions fail until
the setting is fixed -- its mail is never posted under the point's
address instead.

The logic lives in `internal/tosser/points.go`.
