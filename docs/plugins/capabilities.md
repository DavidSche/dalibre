# Plugin capabilities

Status: rollout steps 1–4 and 6 are built (the Get books page, the book
source capability with filters, collections, `get`, and the built-in
Project Gutenberg plugin; a data folder and file settings; the metadata
capability, `permissions.library` and the built-in NeoDB plugin; the
file type capability); the rest is a proposal. Extends plugin API 1
without breaking it; see [design.md](design.md) for the engine, sandbox
and trust model, which this keeps as they are. Plugin authors read
[README.md](README.md).

UI: Figma, Screens › **07 · Get books** (supersedes the per-source
pages in 05) and the Plugins dialog there.

## The idea

A plugin is one package that can **add several capabilities** to
Dalibre. Get books is the first; metadata, library actions and others
follow on the same model.

Obsidian is the model for the package (a manifest, one script, settings,
community list) and the opposite for the UI: an Obsidian plugin gets the
whole app and the DOM. A Dalibre plugin is untrusted code in a sandbox, and
the app draws natively, so:

| Obsidian | Dalibre |
|---|---|
| `manifest.json` + `main.js` | `plugin.json` + `main.js` (as now) |
| `class extends Plugin { onload() { this.addCommand(…) } }`: capabilities registered in code | Capabilities **declared** in `plugin.json`, implemented by exports. Dalibre knows what a plugin adds, and asks the right permissions, before running any of its code |
| Plugins draw views, modals, settings tabs | Plugins **never draw**. They return data; Dalibre's own screens show it (the Get books page, the detail panel, menus, the settings form) |
| `loadData` / `saveData` | `node:fs` over the plugin's own data folder (`/data`) |
| Full access to the vault and the network | Hosts and library access granted per plugin, at install |
| Restricted mode; community list in a GitHub repo | Same: plugins off until the user turns them on; an index repo; install from file always works |
| `minAppVersion` | `api` (as now) |

Why declarative: every capability's UI is the same for all plugins, so
users learn it once; a plugin can't fake a dialog or a sign-in page; the
app can lay out the source picker, filters and settings without loading
the script; and the UI can change without breaking plugins.

## Manifest

`capabilities` becomes an object; the array form (`["source"]`) stays
valid in API 1 and means `{ "bookSource": {} }`.

```json
{
  "id": "gutenberg",
  "name": "Project Gutenberg",
  "version": "0.3.0",
  "api": 1,
  "icon": "globe",
  "description": "70,000 public-domain books",
  "capabilities": {
    "bookSource": {
      "filters": [
        { "key": "language", "title": "Language", "type": "language" },
        { "key": "sort", "title": "Sort", "type": "select",
          "options": [{ "value": "popular", "title": "Most popular" },
                      { "value": "new", "title": "Recently added" }] }
      ],
      "collections": true
    },
    "metadata": { "fields": ["title", "authors", "tags", "cover", "description"] }
  },
  "permissions": {
    "hosts": ["gutendex.com", "www.gutenberg.org", "gutenberg.org"],
    "library": "read"
  },
  "settings": [ … ]
}
```

- `icon`: a name from Dalibre's icon set (Lucide subset), never an image:
  a plugin can't dress up as another app.
- `description`: one line, shown under the name in the source picker.
- Filters are typed (`select` and `language` today; `text` and
  `toggle` when a source needs them) so Dalibre draws them with its own
  controls. In *All sources* only the language filter shows, shared by
  every source that has one. While browsing, only the language shows: a
  collection keeps its own order.

## Capability: Book source (Get books)

The user picks a source on the Get books page, types, browses,
opens a result, downloads. The plugin supplies data for each step:

| Export | Called when | Returns | Required |
|---|---|---|---|
| `search(query)` | the user searches (and "Show more") | `Page` | yes |
| `list(query)` | the search box is empty: the Browse page, a collection's "See all" | `Page` | with `collections` |
| `collections()` | the Browse page opens | `Collection[]` (once per session, cached) | with `collections` |
| `get(item)` | a result is opened in the detail panel | a fuller `Result` | no; else the search result is shown as is |
| `resolve(item, format)` | Download | `DownloadRequest` | no; else `format.url` |

`download` stays with the host, as now: Dalibre streams the file with
progress and cancel, checks it's a book, and imports it. The plugin only
says **where** the file is (`resolve`). That keeps files off the
plugin's side of the sandbox and gives every source the same download
behaviour.

```ts
interface Query {
  text: string;            // as now
  page: number;            // as now
  cursor: string;          // as now
  filters: Record<string, string>; // new: values of the declared filters
  collection?: string;     // new, list() only
}

interface Collection { id: string; title: string }   // "Popular this week"

interface Result {          // as now, plus:
  url?: string;             // the book's page on the site: "Open site"
  fields?: { label: string; value: string }[]; // extra detail rows: "Rights", "Downloads"
}
```

Dalibre, not the plugin, decides:

- **In library**: a result whose identifiers (the plugin's own id,
  ISBN) match a book in the library shows "In library" instead of Get.
- **All sources**: calls `search` on every enabled source in parallel
  (calls into one plugin are still serialized), shows each source's
  first 3 results under its header as they arrive. One source failing
  or needing setup is a state of its
  group, never of the page. This answers the open question in
  design.md: one source by default, all at once when asked.
- **States** from error kinds (unchanged): `auth`/`settings` → Needs
  setup; `rateLimited`/`unavailable` → banner + Retry; the rest → "the
  plugin failed".

Compatibility: an API 1 source plugin today is a book source plugin with
no filters and no collections, and works unchanged.

## Capability: Metadata

calibre's metadata sources. Shows up as **Fetch metadata** in the Edit
metadata dialog (Figma, Screens › 04 · Edit metadata, "Fetch metadata").

| Export | Returns |
|---|---|
| `identify(book)` | candidates, best first, at most 20: `Result`s without `formats` |

`book` is only what the user asked to look up: `{ title, authors,
identifiers, language? }`, or what they typed in the step's search box
(a title, or an ISBN as `identifiers.isbn`). The candidate's `cover` is
an image URL on the plugin's hosts, fetched by Dalibre; there is no
separate `cover` export. Dalibre shows the candidates next to the
current values, one checkbox per field (on for fields that differ and
the candidate fills), merges identifiers, and fills the form with what
the user picks; only Save writes the library. Needs
`permissions.library: "read"`.

With several metadata plugins, the step searches all of them at once, or
the one picked in its source menu; candidates are listed by plugin, then
rank.

Built in: **NeoDB** (`dalibre.neodb`, `plugins/neodb`): neodb.social's
catalog API, no account. By NeoDB id or ISBN when the book has one
(an ISBN search keeps only that edition), else by title and first
author, ranked by title, then authors and language. Candidates carry
NeoDB, Douban, Goodreads, Open Library and Google Books ids.

Built in: **Google Books** (`dalibre.googlebooks`, `plugins/googlebooks`):
the Books API, with the user's own API key as an optional `secret`
setting. By Google id or ISBN when the book has one, else by title and
first author (fielded, then free text), ranked as NeoDB. Without a key,
Google's shared quota running out is an `auth` error, so the app
offers the settings.

## Capability: File type

calibre's file type plugins. The plugin exports `process(file)`, gets a
copy of a book's file in `/work` and returns the path of a new one, or
`null`. Every file Dalibre adds goes through the file type plugins, one
after the other (unless `"onImport": false`), and a failure adds the
file as it was. From the book menu (Figma, 16 · Run plugin on books) the
plugin's `title` runs at once on the books, in the background with the
sidebar's progress; the old file is kept as `ORIGINAL_<format>` and a
toast offers Undo. Choosing the action is the whole decision: no dialog.
A `file` setting gives the plugin a file the user chooses, such as a key.

Hooks, as calibre's: `onPostImport`, `onPostAdd` and `onPostDelete` call
`postimport`, `postadd` and `postdelete` after a file is added, a book is
added and a format is removed, with the book's details (they need
`permissions.library: "read"`). Where calibre's hooks get the database,
ours return `{ metadata }` changes, which Dalibre writes with
`permissions.library: "write"`. calibre's conversion hooks wait for a
conversion pipeline.

## Later capabilities

Each is a new key under `capabilities`, its exports, and a place in
Dalibre's UI. None is needed for Get books; listed so the model is
checked against them.

| Capability | Where it shows | Plugin returns |
|---|---|---|
| `actions` | book menu and ⌘K: "Send to Kindle", "Look up on Goodreads" | `run(actionId, books)` → effects Dalibre applies after confirming: set fields, add tags, open URL, add a format, toast |
| `hooks` | none; runs when a book's details change or it is removed (adding books and formats is the file type hooks') | effects, as `actions` |
| `devices` | "Send to device" targets | a `DownloadRequest`-like upload description |

Effects instead of library writes: a plugin never writes the library
itself. It returns what it wants changed; Dalibre shows and applies it.
`permissions.library: "write"` only allows *proposing* changes without a
confirmation per book.

## Settings and storage

- Setting types add `select`, `toggle`, `number`, `url` to `text` and
  `secret`. Still a form Dalibre draws.
- ✅ A data folder per plugin, through the standard `node:fs` at `/data`
  (64 MB), for tokens and caches that survive restarts. Secrets move to
  the OS keychain together with `secret` settings.
- Sign-in flows (open the browser, receive a callback) stay in Later.

## Permissions and consent

`permissions` grows one key:

| | Means | Consent dialog says |
|---|---|---|
| `hosts` | as now | "Can connect to gutendex.com, …" |
| `localNetwork` | as now | warning tone |
| `library: "read"` | sees the books it is asked about (Metadata, Actions) | "Your library: title, authors, identifiers — only for books you ask it to look up" |
| `library: "write"` | may change books without asking each time | warning tone |

Capabilities are listed in the dialog ("Adds to Dalibre: Get books,
Metadata") so the user sees what installing changes. An update that adds
a capability needing a new permission stays off until granted, as for
hosts today.

## Distribution

As Obsidian's community plugins, minus the trust it implies:

1. Restricted by default: Settings › Plugins has "Turn on plugins" with
   one paragraph on what plugins can do; nothing loads before it.
2. **Get more sources…** (source picker) and Settings › Plugins ›
   Browse read an index: a JSON file in a GitHub repo listing
   `{ id, name, description, repo }`; packages are the repo's release
   assets (`.dalibre-plugin`), checked against a hash in the index.
   Being listed means "reviewed for honesty of its manifest", not
   "endorsed".
3. Install from file stays, and needs no index.
4. Updates: the index lists versions; Dalibre checks on start, at most
   daily.

What the index accepts is still the open question in design.md
(public-domain and licensed sources only?), now with a concrete place to
enforce it.

## Rollout

1. ✅ **Get books page** (Figma 07 · Get books): fixed page, source
   picker, search, browse, detail, download, states; downloads in the
   jobs popover; the Plugins dialog and the permission dialog.
2. ✅ `filters`, `collections` + `list`, `get`, `url`, `fields`; the
   object form of `capabilities`; `icon` and one-line `description`.
   All sources. Project Gutenberg rewritten on gutenberg.org's own OPDS
   catalog and built into Dalibre as `dalibre.gutenberg`.
3. ✅ A data folder per plugin (`node:fs` at `/data`, 64 MB), and `file`
   settings. Still to do: other setting types, keychain.
4. ✅ Metadata capability with `permissions.library`; Fetch metadata in
   Edit metadata; NeoDB built in as `dalibre.neodb`.
5. Index, restricted mode, updates.
6. ✅ File type capability: `process`, on import and from the book menu.
7. Actions and hooks.

Each step ships on its own and keeps earlier plugins working.

## Code

| | |
|---|---|
| `internal/plugin/manifest.go` | `capabilities` object (array still accepted), `icon`, filters; `Reserved` ids |
| `internal/plugin/source.go` | `search`, `list`, `collections`, `get`, `resolve`; checks for `Query.filters`, `Collection`, `fields` |
| `internal/plugin/plugin.go` | `LoadBuiltin`: a plugin from an `fs.FS`, as the built-in ones |
| `plugins/` | the built-in plugins, embedded (`plugins.Builtin`) |
| `internal/app/getbooks.go` | the page's state: sources, lists, browse, get, covers, "In library" |
| `internal/app/view_getbooks.go` | the page: picker, filters, results, shelves, groups, states, detail |
| `internal/app/view_plugins.go` | the Plugins dialog and the permission dialog |
| `internal/app/plugins.go` | loading (built-in and installed), consent, downloads |
| `sdk/dalibre.d.ts`, `plugin.schema.json`, `README.md` | the contract |
| `internal/plugin/metadata.go` | `identify`: `Book`, candidates and their checks |
| `internal/app/fetchmeta.go`, `view_fetchmeta.go` | the Fetch metadata step: search, candidates, comparison, apply |
| `plugins/neodb` | the NeoDB metadata plugin |
| `plugins/googlebooks` | the Google Books metadata plugin |
