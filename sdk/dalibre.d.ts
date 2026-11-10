// Type declarations for Dalibre plugins, plugin API 1.
//
// A plugin is an ES module (ES2023); CommonJS (module.exports) works too.
// A book source exports search and, optionally, collections and list
// (with "collections": true in plugin.json), get and resolve. A metadata
// source exports identify. A reader exports open and declares the formats
// it lays out. In plain JavaScript, put
// this file next to main.js with the template's jsconfig.json and add
// `// @ts-check` to main.js: editors then check your code and complete
// the API. In TypeScript, compile to one module, for example with
// `esbuild main.ts --bundle --format=esm --target=es2023 --outfile=main.js`.

declare namespace Dalibre {
  /** What search is asked for. */
  interface Query {
    /** What the user typed, trimmed. May be empty: return popular or recent books, or nothing. */
    text: string;
    /** The page asked for, from 1. */
    page: number;
    /** The cursor the previous page returned; "" on the first page. */
    cursor: string;
    /** The value of every filter plugin.json declares, by key: one of its options' values. */
    filters: Record<string, string>;
    /** list only: the id of the collection asked for. */
    collection?: string;
  }

  /** A list of books to browse while the search box is empty: "Most popular". */
  interface Collection {
    id: string;
    title: string;
  }

  /** A line of the detail panel: { label: "Rights", value: "Public domain" }. */
  interface Field {
    label: string;
    value: string;
  }

  /** A page of results. */
  interface Page {
    /** At most 100. Return [] when nothing is found. */
    items: Result[];
    /** Whether the user can ask for the next page. */
    hasMore?: boolean;
    /** Handed back as query.cursor for the next page, for APIs that page by token. */
    cursor?: string;
  }

  /** A book the source found. Only id, title and formats are required. */
  interface Result {
    /** Unique within this source, at most 256 characters. */
    id: string;
    title: string;
    authors?: string[];
    /** Plain text or simple HTML. */
    description?: string;
    publisher?: string;
    /** ISO 639: "en", "eng", "zh-CN". */
    language?: string;
    /** "2006", "2006-01", "2006-01-02" or an RFC 3339 time. */
    pubdate?: string;
    series?: string;
    seriesIndex?: number;
    tags?: string[];
    /** As calibre keeps them: { isbn: "…", goodreads: "…" }. Use your plugin's id as a key for its own id. */
    identifiers?: Record<string, string>;
    /** An image URL on a host in permissions.hosts. */
    cover?: string;
    /** The book's page on your site; the detail panel links to it. */
    url?: string;
    /** More lines for the detail panel, at most 12. */
    fields?: Field[];
    /** At least one. */
    formats: Format[];
    /** Anything JSON; handed back unchanged to resolve. */
    extra?: unknown;
  }

  type FormatName =
    | "EPUB" | "KEPUB" | "AZW3" | "AZW" | "MOBI" | "PDF" | "FB2" | "DJVU" | "CBZ" | "CBR" | "CB7"
    | "TXT" | "RTF" | "DOCX" | "ODT" | "HTMLZ" | "LIT" | "PRC" | "PDB" | "LRF" | "SNB" | "TCR";

  interface Format {
    format: FormatName;
    /** Bytes, if known; shown to the user. */
    size?: number;
    /** Where to download it. Leave it out and export resolve when links must be made at download time. */
    url?: string;
  }

  /** Where the host downloads a file from. */
  interface DownloadRequest {
    url: string;
    headers?: Record<string, string>;
    /** The file's name; else the server's, else the title. The extension is fixed to the format. */
    filename?: string;
  }

  interface FetchInit {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
  }

  interface Response {
    status: number;
    statusText: string;
    ok: boolean;
    /** The final URL, after redirects. */
    url: string;
    /** Lowercase names. */
    headers: Record<string, string>;
    text(): string;
    json(): any;
  }

  /** Kinds the app tells the user about differently. */
  type ErrorKind =
    | "auth"        // credentials missing or refused: the app offers the plugin's settings
    | "notFound"    // the book or file is gone
    | "rateLimited" // the service asks to slow down
    | "unavailable" // the service is down
    | "invalid";    // the query can't be searched

  /** What a book source plugin exports. */
  interface Source {
    /** The user searched, or asked for the next page. */
    search(query: Query): Page | Promise<Page>;
    /** With "collections": true, what to browse while the search box is empty. At most 12. */
    collections?(): Collection[] | Promise<Collection[]>;
    /** With "collections": true, a page of query.collection. */
    list?(query: Query): Page | Promise<Page>;
    /**
     * The user opened a result: return all you know about it, for the
     * detail panel. Formats and extra you leave out are kept from item.
     */
    get?(item: Result): Result | Promise<Result>;
    /** Where to download item as format, when format.url won't do. */
    resolve?(item: Result, format: FormatName): DownloadRequest | Promise<DownloadRequest>;
  }

  /** Back-compatible names. */
  type BookSource = Source;

  /**
   * What a metadata source is asked to look up: what the user has for the
   * book, or what they typed in Fetch metadata (then only title, and
   * identifiers.isbn when they typed one). Nothing else of the library is
   * sent.
   */
  interface Book {
    /** May be empty when identifiers are given. */
    title: string;
    authors: string[];
    /** As calibre keeps them, lowercase keys: { isbn: "9780765382030", goodreads: "20518872" }. */
    identifiers: Record<string, string>;
    /** ISO 639, when the book has a language. */
    language?: string;
  }

  /**
   * A metadata candidate: a Result without formats. id and title are
   * required; give every field you know. cover is fetched when the user
   * picks the candidate; fields show next to it.
   */
  type Candidate = Omit<Result, "formats" | "extra">;

  /** What a metadata source exports. */
  interface MetadataSource {
    /** Candidates for book, best first; at most 20. Return [] when nothing matches. */
    identify(book: Book): Candidate[] | Promise<Candidate[]>;
  }

  /** A piece of a chapter as Dalibre extracted it. */
  interface Block {
    kind: "heading" | "paragraph" | "quote" | "divider" | "image" | "table" | "code";
    /** The block's text. Code keeps its line breaks and indentation. */
    text?: string;
    /** heading only, 1–6. */
    level?: number;
    /**
     * image only: names a picture the host keeps, in the book's own file.
     * Pass it through to show the picture; the bytes never enter the
     * sandbox. A plugin may drop or move image blocks.
     */
    src?: string;
    /** image only: what the picture shows, for a reader who can't see it. */
    alt?: string;
    /** image only: the book's line under the picture, when it has one. */
    caption?: string;
    /** table only: its caption, rows and cells of text. */
    table?: Table;
  }

  /** A table of text cells, as a page sets it; pictures stay in their own blocks. */
  interface Table {
    caption?: string;
    rows: TableRow[];
  }

  interface TableRow {
    cells: TableCell[];
  }

  interface TableCell {
    text: string;
    /** A heading cell, set apart from the data. */
    header?: boolean;
    /** How many columns the cell spans, 1–32. */
    colspan?: number;
    /** How many rows the cell spans, 1–32. */
    rowspan?: number;
  }

  /** A footnote or endnote, kept at the end of its chapter. */
  interface Note {
    /** The id its reference points at; optional. */
    id?: string;
    /** The marker the text shows, as [1] or [*]; made from the reference when absent. */
    label?: string;
    text: string;
  }

  /** A chapter, or part of one: continued marks a part that carries on the one before. */
  interface Chapter {
    title?: string;
    /** Its place in the book: 1 a part, 2 a chapter under it; 1–6, made 1 when absent. */
    depth?: number;
    blocks: Block[];
    continued?: boolean;
    /** Footnotes and endnotes, shown after the chapter's text. */
    notes?: Note[];
  }

  /** The text, tables, notes and picture names Dalibre extracted from the book's file. */
  interface Document {
    title?: string;
    /** ISO 639 of the book, when the file names it. */
    language?: string;
    chapters: Chapter[];
    /** The book was cut for length: the page says where it stops. */
    truncated?: boolean;
  }

  /** The book being laid out: what the library knows, and the format read. */
  interface ReadBook {
    id: string;
    title: string;
    authors?: string[];
    /** ISO 639. */
    language?: string;
    format: FormatName;
  }

  interface OpenInput {
    book: ReadBook;
    document: Document;
  }

  /** A book as the reading page shows it. */
  interface Reading {
    /** Else the library's. */
    title?: string;
    author?: string;
    /** At least one, at most 2,000; 60,000 blocks, 20,000 characters a block, 500 notes a chapter. */
    chapters: Chapter[];
    typography?: {
      /** Indent each paragraph's first line, as Chinese and Japanese books do. */
      indent?: boolean;
    };
    /** Dalibre sets it from the document when the book was cut. */
    truncated?: boolean;
  }

  /** What a reader exports. */
  interface ReaderSource {
    /** Lay out the book the user opened: chapters, blocks, typography. */
    open(input: OpenInput): Reading | Promise<Reading>;
  }
}

declare const dalibre: {
  /** The plugin API version of the running Dalibre. */
  readonly api: number;
  readonly plugin: { readonly id: string; readonly version: string };
  /** The user's values for the settings in plugin.json, by key; defaults when not set. */
  readonly settings: Readonly<Record<string, string>>;
  /**
   * The web's fetch, cut down. Only hosts in permissions.hosts, redirects
   * included; at most 50 requests per call and 16 MB per response. Requests
   * run one at a time, so Promise.all doesn't make them parallel.
   */
  fetch(url: string, init?: Dalibre.FetchInit): Promise<Dalibre.Response>;
  /** An Error the app understands: `throw dalibre.error("auth", "The API key was refused")`. */
  error(kind: Dalibre.ErrorKind, message?: string): Error;
  log(...args: unknown[]): void;
};

declare const console: {
  log(...args: unknown[]): void;
  info(...args: unknown[]): void;
  warn(...args: unknown[]): void;
  error(...args: unknown[]): void;
  debug(...args: unknown[]): void;
};

/** For CommonJS plugins; ES modules export their functions instead. */
declare const module: { exports: Partial<Dalibre.Source & Dalibre.MetadataSource & Dalibre.ReaderSource> };
declare const exports: Partial<Dalibre.Source & Dalibre.MetadataSource & Dalibre.ReaderSource>;
