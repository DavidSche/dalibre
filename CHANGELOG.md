# Changelog

Each version's section is the release notes that Dalibre's update window
shows, so write them for readers of the app. `mygo build` needs a
`## <version>` section for the version in `cmd/dalibre/mygo.json`.

## Unreleased

- PDF books open in the reader: each page is drawn when the reader turns
  to it, so a large or scanned book opens without waiting for every page.
  PDFium runs as WebAssembly inside Dalibre, so the build still needs no
  cgo and ships as one binary.

## 0.3.1

- Saving an edited EPUB, KEPUB or TXT book now works on Windows: the
  saved book replaces the old file instead of failing with “Access is denied”.
- Sharing now recognizes a port that another program has on Windows, so
  it says the port is in use instead of showing only the socket error.
- MCP's Add books says a missing file is missing in plain words.

## 0.3.0

- Read books in Dalibre itself: a quiet, full-window page with the
  contents, bookmarks, your place kept, and type you can size, space and
  tint — paper, sepia, green or night.
- EPUB, KEPUB and TXT open in the reader. EPUB illustrations and plates
  are set with the text, with their captions, at the size the book gives
  them: a small formula stays small and sharp, a plate fits the page. A
  chapter's pictures are decoded when it is shown and let go when the
  reader turns on, so a book of thousands of formula pictures stays light;
  up to 5,000 pictures a book are read. Tables are set as tables;
  footnotes and endnotes wait at the end of their chapter, marked in the
  text. The contents keeps the book's own hierarchy, so parts and the
  chapters under them are indented instead of listed flat. Plugins can add
  a reader that lays books out their way; Dalibre extracts the text,
  pictures and tables and draws the page.

## 0.2.2

- Edit an EPUB, KEPUB or TXT book's text: choose Edit book from a book's
  menu, or Edit on one of its formats.
- Fetch metadata finds books on Google Books.
- Choose Light or Dark in Settings › General, or follow the system.

## 0.2.1

- Send books to KOReader over Wi‑Fi: turn on Wireless devices in the
  Sharing button, then choose Calibre › Connect in KOReader.
- On macOS, closing the window keeps Dalibre running; click its Dock icon
  to bring it back.

## 0.2.0

- Let AI agents such as Claude Code, Claude Desktop and Cursor use your
  library over MCP: turn it on from the MCP button in the toolbar.
- Sharing over OPDS moves to its own button in the toolbar.

## 0.1.1

- Report a bug or request a feature from Feedback in the sidebar.

## 0.1.0

- The first release of Dalibre: browse, search and edit your calibre
  library, and get new books from Project Gutenberg and plugins.
