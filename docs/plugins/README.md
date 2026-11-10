# Writing Dalibre plugins

A Dalibre plugin is a folder with a `plugin.json` and a JavaScript (or
TypeScript) module.
Plugin API 1 has three capabilities; a plugin may have any of them:

- A **book source** searches and browses a catalogue and says where each
  book's file is. Dalibre shows every book source in its **Get books**
  page, with the same controls for all, downloads the file and adds the
  book to the user's library, with the metadata the plugin gave.
- A **metadata source** looks books up: given a book's title, authors
  and identifiers, it returns candidates with their details. Dalibre
  shows them in Edit metadata › **Fetch metadata**, next to the book's
  own values, and fills in the fields the user picks.
- A **file type** plugin works on books' files, as calibre's file type
  plugins do: each file Dalibre adds, and the books the user runs it on
  from the book menu. It cleans them up, converts them, or unlocks them
  with the user's own key.

Your plugin gives data and never draws UI.

The [Project Gutenberg plugin](../../plugins/gutenberg) (a book source)
and the [NeoDB plugin](../../plugins/neodb) and [Google Books
plugin](../../plugins/googlebooks) (metadata sources) ship inside
Dalibre; read them as worked examples.

No build step and nothing to install besides the `dalibre-plugin` tool:
plugins are JavaScript or TypeScript modules (ES2023), run sandboxed by
the JavaScript engine built into Dalibre
([moejs](https://github.com/MoeForge/moejs)). They use the standard APIs
(`fetch`, `URL`, `setTimeout`, `TextEncoder`, `node:fs`…), within the
limits of what the user granted.

- [Quick start](#quick-start)
- [plugin.json](#pluginjson)
- [main.js](#mainjs): [`search`](#search), [filters](#filters), [`collections` and `list`](#collections-and-list), [`get`](#get), [`resolve`](#resolve), [`identify`](#identify), [`process`](#process)
- [Runtime](#runtime): [what you can use](#what-you-can-use), [fetch](#fetch), [timers](#timers), [your folders](#your-folders), [the `dalibre` object](#the-dalibre-object), [errors](#errors), [TypeScript](#typescript-and-npm-packages)
- [Permissions and limits](#permissions-and-limits)
- [Testing](#testing)
- [Publishing](#publishing)
- [Compatibility](#compatibility)
- [Guidelines](#guidelines)

## Quick start

```sh
go build ./cmd/dalibre-plugin                    # in the Dalibre repository, for now

dalibre-plugin init my-source --id com.example.books --name "Example Books"
cd my-source
# edit plugin.json and main.js
dalibre-plugin check .
dalibre-plugin search . "dune" --set apiKey=…
dalibre-plugin get . "dune"
dalibre-plugin download . "dune" --format EPUB
dalibre-plugin pack .                            # com.example.books-0.1.0.dalibre-plugin
```

`init` writes `dalibre.d.ts`, `jsconfig.json` and `plugin.schema.json`
next to your code: with `// @ts-check` at the top of `main.js`, VS Code
and other editors check your code and complete the API as you type.

Prefer TypeScript? Write `main.ts` and set `"main": "main.ts"`: Dalibre
runs it as it is. See [TypeScript and npm packages](#typescript-and-npm-packages).

## plugin.json

```json
{
  "$schema": "./plugin.schema.json",
  "id": "com.example.books",
  "name": "Example Books",
  "version": "1.2.0",
  "api": 1,
  "description": "Books from example.com",
  "author": "Ann Author",
  "homepage": "https://github.com/ann/dalibre-example-books",
  "license": "MIT",
  "main": "main.js",
  "icon": "globe",
  "capabilities": {
    "bookSource": {
      "collections": true,
      "filters": [
        { "key": "language", "title": "Language", "type": "language",
          "options": [{ "value": "", "title": "Any language" }, { "value": "en" }, { "value": "fr" }] },
        { "key": "sort", "title": "Sort", "type": "select", "default": "popular",
          "options": [{ "value": "popular", "title": "Most popular" }, { "value": "new", "title": "Newest" }] }
      ]
    }
  },
  "permissions": {
    "hosts": ["api.example.com", "*.cdn.example.com"],
    "localNetwork": false
  },
  "settings": [
    { "key": "apiKey", "title": "API key", "type": "secret", "required": true,
      "description": "From your account page on example.com." }
  ]
}
```

| Field | |
|---|---|
| `id` | **Required.** Permanent and unique: lowercase letters, digits and `-`, in dot-separated parts. Use a domain you control, reversed (`com.example.books`), or your GitHub name (`io.github.ann.books`). Ids starting with `dalibre` are kept for plugins that ship inside Dalibre; a package using one isn't installed. Changing it makes a different plugin. |
| `name` | **Required.** Shown to users; up to 64 characters. |
| `version` | **Required.** [Semantic version](https://semver.org) of your plugin. |
| `api` | **Required.** The plugin API you wrote for: `1`. |
| `description` | One line, shown under your name in the Get books source picker and when the user installs: what the catalogue holds ("Over 70,000 free public-domain ebooks"). |
| `author`, `homepage`, `license` | Shown when the user installs. `license` is an SPDX id. The page's Open site button opens `homepage`. |
| `icon` | One of Dalibre's icons: `globe` (default), `library`, `book-open`, `database`, `network`, `building`, `bookmark`, `layers`. Never an image, so a plugin can't dress up as another app. |
| `main` | The main module, relative to `plugin.json`: a `.js`, `.mjs`, `.ts` or `.mts` file. Default `main.js`. |
| `capabilities` | **Required.** `{"bookSource": {…}}`: `collections` (`true` when you export `collections` and `list`) and up to 4 `filters` ([below](#filters)). `{"metadata": {…}}`: a metadata source ([`identify`](#identify)). `{"fileType": {…}}`: a file type plugin ([`process`](#process)), with an optional `title` (what the book menu calls it, default your name), `onImport` (`false` to run only from the book menu), and `onPostImport`, `onPostAdd`, `onPostDelete` for [hooks](#hooks). `["source"]`, the form API 1 began with, still means a book source with no filters or collections; `["metadata"]` works too. |
| `permissions.hosts` | **Required for a source.** Every host your plugin reaches, through `fetch`, file downloads, covers **and redirects**. `*.example.com` matches any subdomain, not `example.com` itself. |
| `permissions.library` | `"read"`: your plugin sees the details of the books it is asked about. **Required for a metadata source** and for [hooks](#hooks). `"write"`: your hooks may also change those details; the install dialog warns about it. |
| `permissions.localNetwork` | Reach this computer and the local network: a home OPDS or Calibre server. Users are warned. Default `false`. |
| `settings` | Values the user enters: up to 32. `type` is `text` (default), `secret` (masked, hidden in logs, and kept in the system's keychain rather than Dalibre's settings file) or `file`: a file the user chooses, such as a key, copied into `/data`; its value is the path you read it at (`await readFile(dalibre.settings.keyFile)`). `required` settings must be filled before your plugin is called. |

Unknown fields are errors, so a misspelt one is caught by `check`, not
silently ignored.

## main.js

`main.js` (or the module `main` names, such as `main.ts`) is an ES
module, run once when Dalibre loads the plugin. It exports the functions
Dalibre calls:

```js
// @ts-check
import { toResult } from "./lib/map.js";       // your own files, relative paths

/** @param {Dalibre.Query} query @returns {Promise<Dalibre.Page>} */
export async function search({ text, page, cursor }) { … }

/** @param {Dalibre.Result} item @param {Dalibre.FormatName} format */
export async function resolve(item, format) { … }   // optional
```

`export default { search, resolve }` and CommonJS
(`module.exports = { search }`, `exports.search = …`) work too.

- **Imports** are relative paths to `.js`, `.mjs`, `.ts` or `.mts` files
  in your plugin's folder; as in TypeScript, `"./lib.js"` finds `lib.ts`,
  and `"./lib"` finds `lib.ts` or `lib.js`. `node:fs`, `node:fs/promises`
  and `node:buffer` can be imported too. Package names (`"lodash"`) can't:
  bundle them into your code with esbuild.
- Modules are **strict mode**: writing to a read-only property (anything
  in `dalibre`) throws, as in browsers.
- Your functions may be `async`. Keep state across calls in module
  variables (a token, a cache). It lasts until Dalibre quits or reloads
  plugins, **or until a call runs out of time**, after which your module
  is loaded afresh. What must survive goes in [`/data`](#your-folders).

### search

`search(query) → Page` is called when the user searches with your
source, and again with `page + 1` when they ask for more. With **All
sources** picked, every enabled source is searched at once and each
shows its first 3 results under its name.

```ts
query: { text: string, page: number /* from 1 */, cursor: string /* "" at first */,
         filters: { [key: string]: string } /* every declared filter's value */ }

Page:  { items: Result[], hasMore?: boolean, cursor?: string }
```

Return `{ items: [] }` when nothing is found. `text` may be empty: return
popular or new books, or nothing. If your API pages by token, return it as
`cursor`; you get it back in the next query.

A **Result** is one book:

| Field | | |
|---|---|---|
| `id` | **required** | Unique within your source, a string, up to 256 characters. |
| `title` | **required** | |
| `formats` | **required** | At least one `{ format, url?, size? }`. |
| `authors` | | Array of names, "First Last". |
| `description` | | Plain text or simple HTML. |
| `publisher`, `series`, `seriesIndex`, `tags` | | |
| `language` | | ISO 639: `"en"`, `"eng"`, `"zh-CN"`. |
| `pubdate` | | `"2006"`, `"2006-01"`, `"2006-01-02"` or an RFC 3339 time. |
| `identifiers` | | `{ isbn: "…", goodreads: "…" }`, as calibre keeps them. Add your own id under your plugin's name, so users can find the book's page later. |
| `cover` | | An image URL on one of your hosts. Dalibre shows it in the results and uses it if the file has no cover. |
| `url` | | The book's page on your site; the detail panel links to it. |
| `fields` | | Up to 12 more lines for the detail panel: `[{ label: "Rights", value: "Public domain" }]`. |
| `extra` | | Anything JSON. Dalibre hands it back unchanged to `resolve`. |

A format is `{ format, url?, size? }`: `format` is one of `EPUB`, `KEPUB`,
`AZW3`, `AZW`, `MOBI`, `PDF`, `FB2`, `DJVU`, `CBZ`, `CBR`, `CB7`, `TXT`,
`RTF`, `DOCX`, `ODT`, `HTMLZ`, `LIT`, `PRC`, `PDB`, `LRF`, `SNB`, `TCR`
(any case); `size` is in bytes; `url` is where to download it.

**Results are checked.** A result missing `id`, `title` or `formats`, or
with an unknown format, is dropped; an invalid `cover` or `pubdate` is
ignored; an unknown field (`author` for `authors`) is ignored. Each gives a
warning, which `dalibre-plugin search` prints and Dalibre logs. Users never
see warnings, so check your plugin's output with the CLI.

Dalibre marks a result **In library** when one of its `identifiers`
matches a book the user has, so give your own id and the ISBN.

### Filters

Filters are drop-downs under the search box. Declare them in
`capabilities.bookSource.filters`; Dalibre draws them and passes their
values in `query.filters`, every declared filter always with one of its
options' values (the `default`, else the first, until the user picks).

| Field | |
|---|---|
| `key` | The name in `query.filters`. |
| `title` | Shown on the drop-down. |
| `type` | `"select"`: options with titles. `"language"`: options are ISO 639 codes and Dalibre names them; `""` with a title is "any". In All sources, only a language filter shows, shared by every source that has one. |
| `options` | 1 to 64 `{ value, title? }`. |
| `default` | The option chosen at first. |

### collections and list

With `"collections": true`, the page shows your collections as shelves
of covers while the search box is empty, each with See all.

```js
export function collections() {
  return [{ id: "popular", title: "Most popular" }, { id: "new", title: "Recently added" }];
}

export async function list({ collection, page, cursor, filters }) {
  // a Page of that collection, as search returns
}
```

`collections()` is called once per session; return at most 12.
`list(query)` gets the same query as `search`, with `collection` set and
`text` empty.

### get

`get(item) → Result` is optional, and called when the user opens a
result in the detail panel. Return everything you know: description,
subjects, language, file sizes, `url`, `fields`. It costs a request per
book opened, so `search` can return only what its listing has. `id` is
kept; formats and `extra` you leave out are kept from `item`.

### resolve

`resolve(item, format) → { url, headers?, filename? }` is optional.
Without it, Dalibre downloads `format.url`. Export it when links expire,
need a second request, or need headers:

```js
exports.resolve = async function (item, format) {
  const res = await fetch(`https://api.example.com/books/${item.id}/download?format=${format}`, {
    headers: { Authorization: `Bearer ${dalibre.settings.apiKey}` },
  });
  if (res.status === 404) throw dalibre.error("notFound");
  const { url } = await res.json();
  return { url, headers: { Authorization: `Bearer ${dalibre.settings.apiKey}` } };
};
```

If you export `resolve`, it is called for every download. `item` is the
result as `search` returned it (after checking), `extra` included.
`format` is upper case. The file name comes from `filename`, else the
server's `Content-Disposition`, else the title; its extension is always
the format's.

Dalibre, not your script, downloads the file: it streams it to disk with
progress and cancel, follows redirects within your hosts, refuses an
HTML page (a login or error page) where a book was expected, and turns
HTTP 401/403/404/429 into the errors below.

### identify

A metadata source declares `"metadata"` in its capabilities and
`"library": "read"` in its permissions: it sees the books the user looks
up with it, and the install dialog says so.

```json
"capabilities": { "metadata": { "fields": ["title", "authors", "cover", "description"] } },
"permissions": { "hosts": ["api.example.com"], "library": "read" }
```

`fields` (optional) lists what you can fill, from `title`, `authors`,
`series`, `tags`, `publisher`, `pubdate`, `language`, `identifiers`,
`cover` and `description`.

`identify(book) → Candidate[]` is called when the user opens Fetch
metadata or searches in it. `book` is
`{ title, authors, identifiers, language? }`: the book's own values, or,
when the user typed something else, `{ title: "what they typed" }` or
`{ identifiers: { isbn } }` when they typed an ISBN. Nothing else of
the library is sent.

```js
export async function identify({ title, authors, identifiers }) {
  const q = identifiers.isbn || [title, authors[0]].filter(Boolean).join(" ");
  const res = await fetch(`https://api.example.com/books?q=${encodeURIComponent(q)}`);
  const { books } = await res.json();
  return books.map((b) => ({
    id: b.key, title: b.title, authors: b.authors, publisher: b.publisher, pubdate: b.date,
    identifiers: { isbn: b.isbn, example: b.key }, cover: b.image, description: b.summary,
  }));
}
```

Return candidates best first, at most 20, `[]` when nothing matches. A
candidate is a [result](#search) without `formats`: `id` and `title` are
required, everything else is optional and shown next to the book's
value. Give everything you know in `identify`: there is no second call
for details. Dalibre fetches `cover` when the candidate is shown;
`identifiers` are added to the book's (keys are lower-cased), so add
your own id under your plugin's name. Rank with what you are given: an
ISBN match first, then the title, then the authors.

### process

A file type plugin declares `"fileType"` in its capabilities and exports
`process(file)`. It gets a copy of a book's file and returns a new one:

```js
import { readFile, writeFile } from "node:fs/promises";

export async function process(file) {
  if (file.format !== "EPUB") return null;     // not for us: leave it
  const cleaned = clean(await readFile(file.path));
  if (!cleaned) return null;                   // nothing to change
  await writeFile(file.path, cleaned);
  return { path: file.path, report: ["Removed 3 unused fonts"] };
}
```

- `file` is `{ path, name, format, reason, book? }`: `path` is the copy in
  `/work`, `reason` is `"import"` as Dalibre adds the file or `"manual"`
  from the book menu, which also gives `book` (`{ title, authors,
  identifiers }`).
- Return `null` to leave the file, or the new file's path (in `/work` or
  `/data`, named with its format's extension), or `{ path, report }`.
  Every added file comes to you: return `null` fast for others.
- **As files are added** (dropped, Add books, Get books, Add format, an
  AI agent), every file type plugin runs, one after the other, unless it
  says `"onImport": false`. One that throws is passed over: the file is
  added as it was, and the user is told.
- **From the book menu**, your `title` runs at once on the book or the
  selection, in the background, on each book's first format among EPUB,
  AZW3, MOBI, PDF…. Dalibre keeps the old file as `ORIGINAL_EPUB`, and a
  toast offers Undo. Throw `dalibre.error("auth", …)` when you need
  something from the user, such as a key: the run stops and the user is
  offered your settings.
- You get the whole file and memory isn't limited; the call has 10
  minutes. Try it with `dalibre-plugin process . "My Book.epub"`.

### Hooks

As calibre's file type plugins, yours can ask to be called after the
library changed, with `permissions.library`:

| Ask with | Export | Called after | calibre |
|---|---|---|---|
| `onPostImport` | `postimport(event)` | a file was added to a book | `on_postimport` |
| `onPostAdd` | `postadd(event)` | a book was added | `on_postadd` |
| `onPostDelete` | `postdelete(event)` | a format was removed | `on_postdelete` |

`event` is `{ book, format?, file?, files? }`: `book` has the book's
details (`id`, `title`, `authors`, `tags`, `identifiers`, `formats`…),
`postimport` gets the `format` and `file` added (a copy in `/work`),
`postadd` the new book's `files`, `postdelete` the `format` removed.
Return `null`, or `{ metadata: { tags: [...book.tags, "Unlocked"] } }`:
where calibre's hooks write the database, yours say what to change and
Dalibre writes it, with `"library": "write"`. Hooks run in the
background after the change; one that fails is logged. A plugin with only
hooks (`"onImport": false`, no `process`) isn't in the book menu.
calibre's conversion hooks have no counterpart: Dalibre doesn't convert
through plugins.

## Runtime

### What you can use

| | |
|---|---|
| Network | `fetch`, `Request`, `Response`, `Headers`, `AbortController` |
| Streams | `ReadableStream`, `WritableStream`, `TransformStream`, `TextDecoderStream`, `TextEncoderStream` |
| Text and data | `URL`, `URLSearchParams`, `TextEncoder`, `TextDecoder`, `atob`, `btoa`, `structuredClone`, typed arrays, `Buffer` |
| Time | `setTimeout`, `setInterval`, `setImmediate` and their `clear…`, `queueMicrotask`, `performance.now()` |
| Files | `node:fs`, `node:fs/promises` over [your folders](#your-folders) |
| Formatting | `Intl`, with the data of ten languages (en, zh, ja, ko, de, fr, es, pt, ru, it) |
| Logging | `console.log/info/warn/error/debug`: to Dalibre's log and the CLI's stderr, with secret settings masked |

Not there: `process`, `require` and Node's other modules, the DOM
(`document`, `DOMParser`), `crypto`, `Blob`, `FormData`,
`CompressionStream`, `WebAssembly`. Bundle a pure-JavaScript library when
you need one of them (hashing, HTML parsing, zip).

### fetch

```js
const res = await fetch(`https://api.example.com/search?q=${encodeURIComponent(text)}`);
if (!res.ok) throw dalibre.error("unavailable", `example.com answered ${res.status}.`);
const data = await res.json();
```

- Only hosts in `permissions.hosts`, redirects included, and not the
  [local network](#permissions-and-limits) unless you ask for it.
- At most 50 requests per call, 10 redirects per request, 16 MB per
  response. Books are downloaded by Dalibre: return their URL.
- Cookies are kept as a browser keeps them, for your plugin alone and in
  memory, and sent with later requests and Dalibre's downloads: sign in
  by posting a form once, and stay signed in until the plugin is loaded
  again.
- Dalibre sets the `User-Agent`. Requests run in parallel.
- A refused request, a network failure or a response over the limit
  rejects with a `TypeError` (`"fetch failed"`) whose `cause` says why.
  Uncaught, it counts as `permission` (a host you didn't declare) or
  `unavailable`.

### Timers

A call ends when the promise your export returns settles; until then
Dalibre runs your timers, fetches and file operations, so
`await new Promise((r) => setTimeout(r, 1000))` waits a second. The call's
60 seconds include the waiting. A timer still set when the call settles
fires during your next call; an exception in a timer's callback fails
the call in progress.

### Your folders

Through `node:fs` your plugin sees two folders of its own, and nothing
else: `/data` keeps what must survive restarts, such as a sign-in token,
a cache or file settings; `/work` holds the files `process` is handed,
emptied after each call.

```js
import { readFile, writeFile } from "node:fs/promises";

const token = await readFile("/data/token", "utf8").catch(() => "");
await writeFile("/data/token", newToken);
```

Relative paths are relative to `/data`, and nothing else can be reached
(`EACCES`). `/data` holds 64 MB (`ENOSPC` when full); only your plugin
sees the folders, and uninstalling the plugin deletes them. With
`dalibre-plugin` they are in `.data` in your plugin's folder, which
`pack` leaves out. There are no streams or file handles
(`createReadStream`, `fs.open`): read and write whole files.

### The dalibre object

What only Dalibre can give you:

| | |
|---|---|
| `dalibre.settings` | The user's values for your settings, by key (defaults where unset). Read-only. |
| `dalibre.error(kind, message?)` | An `Error` Dalibre understands; see below. |
| `dalibre.plugin` | `{ id, version }` from your manifest. |
| `dalibre.api` | The plugin API version of the running Dalibre. |

Everything in the `dalibre` object is frozen.

### Errors

Throw `dalibre.error(kind, message)` so Dalibre can tell the user what to
do:

| kind | When | Dalibre |
|---|---|---|
| `auth` | Credentials missing, expired or refused | Offers your plugin's settings |
| `rateLimited` | The service asks you to slow down (429) | "Try again in a while" |
| `unavailable` | The service is down or answers oddly | "Try again later" |
| `notFound` | The book or file no longer exists | Says so |
| `invalid` | The query can't be searched (too short…) | Shows your message |

Anything else thrown (a `TypeError`, a plain `Error`) is reported as a
bug in the plugin, with your message; the CLI prints its stack. A
`fetch` you don't catch counts as `unavailable` (the network) or
`permission` (a host you didn't declare). Messages are shown to users:
write them for readers, in a sentence.

### TypeScript and npm packages

Set `"main": "main.ts"`: Dalibre runs TypeScript as it is, dropping the
types, and stack traces point into your `.ts` files. TypeScript that
changes what the code does at run time can't be used (`enum`, namespaces
with values, constructor parameter properties, decorators). Check types
in your editor: `dalibre.d.ts` and `jsconfig.json` (or a `tsconfig.json`
with the same options) give `Dalibre.*` and the standard globals; add
`@types/node` for `node:fs` and `Buffer`.

npm packages are bundled in, as long as they need nothing from the list
of what's not there; pure-JavaScript ones such as `fflate` (zip) and
`@noble/ciphers` do. Leave Node's modules to Dalibre:
`esbuild main.ts --bundle --format=esm --target=es2023 --external:'node:*' --outfile=main.js`,
then ship `main.js`. `self` is the global object, as in a Web Worker,
for libraries that look for it.

## Permissions and limits

Plugins are code from strangers, so Dalibre holds them to what they
declare:

- **Hosts.** Every request, redirect, download and cover must be to a
  host in `permissions.hosts`, over `http` or `https`. Credentials in
  URLs (`https://user:pw@…`) are refused; send them in headers.
- **Local network.** Without `localNetwork`, addresses on the user's
  computer or network (`127.0.0.1`, `192.168.x.x`, `10.x.x.x`, `fe80::`…)
  are refused **after** name resolution, so listing a host that resolves
  to one doesn't help.
- **Consent.** Users grant your permissions when they install. An update
  asking for **more** (a new host, or the local network) is installed but
  stays off until the user grants it again; an update asking for the same
  or less installs silently. So list the hosts you need up front,
  `*.cdn.example.com` rather than one CDN node.

| Limit | |
|---|---|
| One call (`search`, `list`, `get`, `resolve`…) | 60 seconds; then the script is stopped |
| One `process` call | 10 minutes |
| Memory | Not limited, as calibre doesn't limit its plugins |
| Requests per call | 50 |
| One `fetch` response | 16 MB (files go through the download) |
| Redirects per request | 10 |
| `/data` | 64 MB |
| A file setting's file | 16 MB |
| Results per page | 100 |
| Download | 2 GB |
| Cover | 10 MB |
| Scripts | 4 MB, all modules together |
| Package | 32 MB unpacked, 1000 files |

Calls into one plugin run one at a time; different plugins run in
parallel. A plugin that throws or times out only fails that call; the
next one runs normally (after a timeout, with the module loaded afresh).

## Testing

`dalibre-plugin` runs your plugin exactly as Dalibre does: the same
interpreter, checks, permissions and limits.

```sh
dalibre-plugin check .                        # manifest, script loads, exports, what it asks for
dalibre-plugin search . "dune"                # results, warnings, and how to get the next page
dalibre-plugin search . "dune" --page 2 --filter language=fr
dalibre-plugin search . "dune" --json         # exactly what Dalibre gets
dalibre-plugin collections .                  # what the browse page shows
dalibre-plugin list . popular --page 2
dalibre-plugin get . "dune" --item 2          # what the detail panel shows
dalibre-plugin identify . "Dune" --author "Frank Herbert"   # what Fetch metadata shows
dalibre-plugin identify . --isbn 9780441172719 --json
dalibre-plugin process . "My Book.epub" -o out  # what a file type plugin makes of a file
dalibre-plugin download . "dune" --item 3 --format PDF -o /tmp
```

`--set key=value` gives a setting; otherwise the values saved in Dalibre
for your plugin's id are used. Logs and warnings go to stderr.

To try it in the app, use Settings › Plugins › Add plugin › From a
folder… and pick your plugin's folder. Dalibre loads the plugin from
that folder, not a copy, and reloads it a moment after you save a change
(hidden files and `node_modules` aside). Remove takes it out of Dalibre
and leaves the folder alone. `dalibre-plugin install` takes a folder or
a package and installs a copy; restart Dalibre after it.

Your own AI agent can do this loop for you over [MCP](../mcp/README.md#write-a-plugin): it starts
the plugin, adds its folder, calls its functions and reads their
results, logs and errors. You still allow the hosts it reaches.

## Publishing

`dalibre-plugin pack .` makes `<id>-<version>.dalibre-plugin`: a zip of
your folder, leaving out hidden files and `node_modules`. Users install
the file. Publish it as a GitHub release asset, with your source next to
it.

Users can install straight from your repository: Settings › Plugins ›
Install from GitHub, with `owner/repo` or a github.com link. They pick
one of your releases (the latest by default) or a branch:

- A release installs its `.dalibre-plugin` asset; a release without one
  installs the repository as tagged.
- A branch installs the repository at the branch's latest commit, so
  keep `plugin.json` at the repository's root, or in a `plugin` folder.
  There is no build step: commit what `main.js` imports.

Dalibre checks for updates at launch and once a day: a newer release
that isn't a pre-release, or a new commit on the branch followed. An
update that asks for no new permissions can install by itself; one that
asks for more waits for the user. Bump `version` in each release.

Users see your name, author, description and what you ask for before
they install. A plugin that asks for little, says what it does and links
its source gets installed.

## Compatibility

- Within plugin API 1, Dalibre only adds: new optional fields in what you
  receive, new optional fields you may return, new functions on
  `dalibre`. Nothing you rely on changes or goes away. Check for a new
  feature before using it (`if (dalibre.somethingNew)`).
- Fields you return that Dalibre doesn't know yet are ignored with a
  warning, never an error, so returning extra data is safe.
- A breaking change will be API 2, announced in Dalibre's release notes,
  with API 1 plugins kept running alongside it for a transition period.
- A plugin declaring a newer `api` than the user's Dalibre isn't loaded,
  and the user is told to update Dalibre.

**Moving from `dalibre.fetch`.** Earlier versions of API 1 had
`dalibre.fetch` and `dalibre.log`; they are gone, for the standard
`fetch` and `console`. Replace `dalibre.fetch(` with `fetch(`, and await
the body: `res.json()` and `res.text()` now return promises
(`await res.json()`).

## Guidelines

- **Only sources you may use.** Publish plugins for catalogues whose
  terms allow it: public-domain libraries, stores and services the user
  has an account with, the user's own servers. Dalibre doesn't host,
  review or endorse plugins.
- **Be polite.** One request per search where the API allows it; cache
  what doesn't change (in module variables, or the data folder); honour
  `429` by throwing `rateLimited` rather than retrying in a loop.
- **Fill in metadata.** Users see results before they download: authors,
  language, size and cover make the choice easy. Add your own id to
  `identifiers`.
- **Ask only for what you need.** Exact hosts beat wildcards; avoid
  `localNetwork` unless the plugin is for a server at home.
- **Never log secrets or send them to a host other than the one they're
  for.**
