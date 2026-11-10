# Changelog

Each version's section is the release notes that Dalibre's update window
shows, so write them for readers of the app. `mygo build` needs a
`## <version>` section for the version in `cmd/dalibre/mygo.json`.

## 0.4.0

- Plugins have their own window, from the toolbar's Plugins button, with
  a Marketplace of calibre's most used plugins and the community's.
- Plugin actions run on the books you choose or the whole library, from
  the book menu or the sidebar's Tools.
- Removing books works in libraries made before calibre 8.

## 0.3.0

- File type plugins work on books' files, as calibre's do: when books are
  added, or from a book's menu.
- AI agents can write, try and install plugins over MCP.
- Plugins use standard APIs (fetch, timers, node:fs) and can be written in
  TypeScript; dalibre.fetch and dalibre.log are removed.
- Plugin secrets such as API keys need to be entered again.
- Dalibre checks for updates every hour.

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
