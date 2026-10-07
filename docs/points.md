# Points: your own reader app (FidoMail and the like)

**English** · [Deutsch](points.de.md)

A point is a reader system under this BBS's own node address:
`21:3/194.1` is point 1 of `21:3/194`. Offline reader apps such as
FidoMail on iOS work as points -- they call the BBS over BinkP, fetch
the areas they subscribed to and hand over what was written in them.

## Setting one up

In **Admin → FTN → Uplinks (Nodes/Points) → Nodes / Points**, add one entry per
network the reader should take part in:

| Field | Value |
| --- | --- |
| Address | the point address, e.g. `21:3/194.1` (fsxNet) or `954:700/14.1` (HobbyNet) |
| Host | a label (e.g. `fidomail`); the reader calls in, it's never dialed. Area grants are kept per label: one label for all entries shares one area list, one per entry gives each network its own |
| Session password | the reader's BinkP password -- the same on every entry |
| Areafix password | for the reader's Areafix requests (`+AREA`, `%LIST`) |
| Network | the network of that address |
| Downlink | on; **Hold** on too, since a reader can't be called |

The reader is configured the other way round: this BBS as its boss
node (host and BinkP port, e.g. `bbs.example.com:24554`), its point
addresses, the same password. It subscribes its areas through
Areafix; the areas ticked for it in the web admin (area grants, kept
per host label) are the ones it may subscribe.

A point, unlike a node, only gets what's its own:

- netmail addressed to it, and never anyone else's -- this system's
  outgoing netmail and echomail still go to the hubs;
- the areas it subscribed to: from two weeks before the subscription
  on, each message once (tracked per point, since SEEN-BY can't name
  points).

The logic lives in `internal/tosser/points.go`.
