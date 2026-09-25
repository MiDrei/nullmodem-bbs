# Third-party software in the container image

NullModem BBS itself is this repository. The container image built from
the `Dockerfile` also carries programs that are not part of it; they run
as separate processes and are not linked into the BBS.

## sexyz (Synchronet)

Zmodem file and QWK transfers over Telnet/SSH run Synchronet's `sexyz`
(see [building-sexyz.md](building-sexyz.md)). The image builds it from
source, pinned to Synchronet commit
`7cf7f2fc56d8383aeb6cd35639977f3815afe7ce`, unmodified.

- License: GNU GPL, version 2 or later; the xpdev and hash libraries it
  links are LGPL 2.1, `zmodem.c` is under a BSD-style license, and the
  MD5 code carries RSA Data Security's notice.
- In the image, `/usr/local/share/doc/sexyz/` holds the notice
  ([`third_party/sexyz/NOTICE`](../third_party/sexyz/NOTICE) in this
  repository), the license texts (`COPYING`, `COPYING.LESSER`,
  `LICENSE.zmodem`, `LICENSE.md5`) and the complete corresponding
  source it was built from (`sexyz-source-7cf7f2fc.tar.gz`, which
  rebuilds with just `build-essential`).
- Upstream: <https://gitlab.synchro.net/main/sbbs>

Changing `SBBS_COMMIT` in the `Dockerfile` means updating the commit in
`third_party/sexyz/NOTICE` and here as well.

## Debian packages

The runtime image is `debian:trixie-slim` plus `dosbox-x` and
`ca-certificates` from Debian; their licenses and copyright notices are
in `/usr/share/doc/<package>/copyright` inside the image, as Debian
ships them.
