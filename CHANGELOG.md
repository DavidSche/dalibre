## 0.4.0

- Merged upstream djyde/dalibre 0.4.0:
  - Plugins have their own window, from the toolbar's Plugins button, with
    a Marketplace of calibre's most used plugins and the community's.
  - Plugin actions run on the books you choose or the whole library, from
    the book menu or the sidebar's Tools.
  - Removing books works in libraries made before calibre 8.

## 0.3.2

- Merged upstream djyde/dalibre 0.3.0:
  - File type plugins work on books' files, as calibre's do: when books are
    added, or from a book's menu.
  - AI agents can write, try and install plugins over MCP.
  - Plugins use standard APIs (fetch, timers, node:fs) and can be written in
    TypeScript; `dalibre.fetch` and `dalibre.log` are removed.
  - Plugin secrets such as API keys need to be entered again.
  - Dalibre checks for updates every hour.
- The reader plugin runs alongside the new plugin runtime.
- Read MOBI books in the reader: PalmDOC compression, the contents
  page, pictures, tables and notes. Encrypted books and books using
  HUFF/CDIC compression say why they cannot be read instead of
  failing silently.
- Pictures now keep the place and size the book gives them: a picture
  beside words stays in the line, and the book's width or height, a length
  or a share of the page, is honored. A large picture file no longer makes
  a plate fill more of the page than the book asks for.

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
