---
title: KOReader
description: Send books from Dalibre to KOReader over Wi‑Fi. KOReader connects to Dalibre as it connects to calibre, with its built-in Calibre plugin. Also available as plain Markdown at /docs/koreader.md.
---

KOReader can connect to Dalibre the way it connects to calibre: its
built-in **Calibre** plugin finds Dalibre on your network, and you send
books to it from Dalibre's book menu. Nothing needs installing on the
reader.

Dalibre calls this **Wireless devices**: it serves calibre's own wireless
device connection, so other reading apps that connect to calibre over
Wi‑Fi can connect too. KOReader is the one this guide, and Dalibre, are
checked with.

Wireless devices are in Dalibre 0.2.1 and later.

## Turn it on

Click the **Sharing** button (the Wi‑Fi icon) in the toolbar and turn on
**Wireless devices**, or open Settings › Wireless devices. The popover shows the
connection's address, as `192.168.1.20:9090`, and what it is doing:

| Status | Meaning |
|---|---|
| Waiting for a reader | Ready; no reader is connected. |
| KOReader (Kobo Libra 2) · 128 books | A reader is connected, with that many books from calibre or Dalibre. |
| Sending 2 of 5… | Books are on their way. |

Dalibre must be open while you connect and send.

## Connect from KOReader

1. Make sure the reader and your computer are on the same Wi‑Fi network.
2. In KOReader, open the Tools menu › **Calibre** › **Connect**. The
   first time, KOReader asks for a folder to keep the books in (later:
   **Wireless settings** › **Set inbox folder**).
3. KOReader finds Dalibre and connects. Dalibre's popover shows it.

If KOReader says it can't find a calibre instance, give it the address
instead: **Calibre** › **Wireless settings** › **Server address** ›
**Manual**, with the address and port from Dalibre's popover. Some networks keep devices from
finding each other.

KOReader treats Dalibre as calibre: its messages say "calibre", and the
books it got from calibre before are listed as on the reader.

## Send books

With a reader connected, right-click a book, or the books you chose, and
pick **Send to KOReader (…)**. Each book goes in the first format KOReader
asks for that the book has: EPUB, unless you picked another in
KOReader's **Calibre** › **Wireless settings** › **File formats**. A message says how many were sent, and names any book in no
format KOReader reads.

Books go to KOReader's Calibre folder as `Author Sort/Title - Authors.epub`,
the same names calibre gives them.

**Remove from KOReader (…)**, in the same menu, deletes the chosen books
from the reader, when it has them.

When a book's metadata changes in Dalibre, the reader gets the new title,
authors, tags and series the next time it connects, or at once while it
is connected, as with calibre. KOReader uses them in its calibre search.

## Password and port

Settings › Wireless devices:

- **Require a password** makes readers give it. Set the same password in
  KOReader, under **Calibre** › **Wireless settings** › **Set password**. A reader with a
  wrong password is turned away, and the popover says so.
- **Port** is where Dalibre listens. It is calibre's, 9090, unless you
  pick another. When calibre or another app has it, Dalibre listens on a
  free one, and KOReader still finds it on its own.

Only one reader connects at a time; another is told Dalibre is busy.

## Troubleshooting

- **KOReader can't find Dalibre.** Use the address from the popover.
  On macOS, allow Dalibre's incoming connections if the firewall asks.
  When calibre is open with its wireless connection on, KOReader may find
  calibre first: turn calibre's off.
- **"Update the reader's app to connect".** The reader's KOReader is too
  old to stream books; update it.
- **A book wasn't sent.** The message says why: no format KOReader reads,
  or no space left on the reader.

## How it works

Dalibre serves calibre's own wireless device protocol, the one calibre's
"Connect/share › Start wireless device connection" serves. It answers
KOReader's discovery broadcast on the first free port of 54982, 48123,
39001, 44044 and 59678, and listens for the reader on TCP.
