# Changelog

Each version's section is the release notes that Dalibre's update window
shows, so write them for readers of the app. `mygo build` needs a
`## <version>` section for the version in `cmd/dalibre/mygo.json`.

## 0.2.0

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

## 0.1.1

- Feedback, above Settings in the sidebar, reports a bug or requests a
  feature on GitHub, with Dalibre's version and your system filled in for
  bug reports, or shows the issues already open.

## 0.1.0

The first release of Dalibre, a desktop library for e-books that reads and
writes calibre libraries.

- Browse, search, filter and sort your calibre library, as a grid of covers
  or a list.
- Import books, and fetch their metadata from sources such as NeoDB.
- Get books from Project Gutenberg and from sources that plugins add.
- Switch between libraries, and share books.
- Dalibre updates itself: it checks for new versions in the background, and
  from Settings › General or, on macOS, Dalibre > Check for Updates….
