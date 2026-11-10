// @ts-check
// The built-in reader. Dalibre extracts a book's text and this plugin lays
// it out: it drops the boilerplate that public-domain and converted books
// carry, names chapters that came without a name, joins paragraphs that a
// converter split, and says how the page should set the text. The page
// itself — type, color, progress, where you left off — is Dalibre's.

/** Boilerplate markers of Project Gutenberg and the like. */
const START = /\*\*\*\s*START OF (?:THE|THIS) PROJECT GUTENBERG[^*]*\*\*\*/i;
const END = /\*\*\*\s*END OF (?:THE|THIS) PROJECT GUTENBERG[^*]*\*\*\*/i;
const END_LINE = /^\s*End of (?:the )?Project Gutenberg/i;

/**
 * @param {Dalibre.OpenInput} input
 * @returns {Dalibre.Reading}
 */
export function open({ book, document }) {
  const chapters = [];
  for (const ch of document.chapters) {
    chapters.push({
      title: clean(ch.title),
      depth: ch.depth || 1,
      continued: ch.continued === true,
      notes: (ch.notes || []).map(copyNote),
      blocks: ch.blocks.map(copy),
    });
  }

  const body = stripBoilerplate(chapters);
  const headers = runningHeaders(body);
  const out = [];
  for (const ch of body) {
    const blocks = normalizeBlocks(ch.blocks, headers);
    let title = ch.title;
    if (!title) title = takeTitle(blocks);
    else if (blocks.length > 0 && sameText(blockText(blocks[0]), title) && blocks[0].kind === "heading") {
      blocks.shift();
    }
    if (blocks.length === 0) continue;
    out.push({ title, depth: ch.depth || 1, continued: ch.continued && out.length > 0, notes: ch.notes || [], blocks });
  }
  if (out.length === 0) {
    // Nothing survived the cleanup: show the text as it came.
    for (const ch of document.chapters) {
      out.push({
        title: clean(ch.title),
        depth: ch.depth || 1,
        continued: ch.continued === true,
        notes: (ch.notes || []).map(copyNote),
        blocks: ch.blocks.map(copy),
      });
    }
  }

  const lang = (book.language || document.language || "").toLowerCase();
  return {
    title: clean(book.title),
    author: (book.authors || []).map(clean).filter(Boolean).join(", "),
    chapters: out,
    typography: { indent: /^(zh|zho|chi|ja|jpn|ko|kor)/.test(lang) },
    truncated: document.truncated === true,
  };
}

/** @param {Dalibre.Block} b @returns {Dalibre.Block} */
function copy(b) {
  const out = { kind: b.kind, text: b.text || "", level: b.level || 0 };
  if (b.kind === "image") {
    out.src = b.src || "";
    out.alt = b.alt || "";
    out.caption = b.caption || "";
  }
  if (b.kind === "table" && b.table) out.table = copyTable(b.table);
  return out;
}

/** @param {Dalibre.Note} n @returns {Dalibre.Note} */
function copyNote(n) {
  return { id: n.id || "", label: clean(n.label), text: (n.text || "").trim() };
}

/** @param {Dalibre.Table} t @returns {Dalibre.Table} */
function copyTable(t) {
  return {
    caption: clean(t.caption),
    rows: (t.rows || []).map((r) => ({
      cells: (r.cells || []).map((c) => ({
        text: (c.text || "").trim(),
        header: c.header === true,
        colspan: c.colspan || 1,
        rowspan: c.rowspan || 1,
      })),
    })),
  };
}

/** @param {string} s */
function clean(s) {
  return (s || "").replace(/\s+/g, " ").trim();
}

/** Code keeps its lines and the spaces that indent them. */
function codeText(s) {
  return (s || "")
    .replace(/\r\n?/g, "\n")
    .replace(/\u00a0/g, " ")
    .replace(/[ \t]+$/gm, "")
    .replace(/^\n+|\n+$/g, "");
}

/** @param {Dalibre.Block} b */
function blockText(b) {
  if (b.kind === "divider" || b.kind === "image" || b.kind === "code") return "";
  if (b.kind === "table") return clean((b.table && b.table.caption) || "");
  return clean(b.text);
}

/** @param {string} a @param {string} b */
function sameText(a, b) {
  const norm = (s) => s.toLowerCase().replace(/[\s"'“”‘’《》「」.,;:!?、，。；：！？—–-]+/g, "");
  return norm(a) !== "" && norm(a) === norm(b);
}

/**
 * Drops what a Project Gutenberg ebook wraps its text in: the license
 * before the start marker and after the end marker.
 * @param {Dalibre.Chapter[]} chapters
 */
function stripBoilerplate(chapters) {
  const hasStart = chapters.some((ch) => ch.blocks.some((b) => START.test(b.text || "")));
  const out = [];
  let started = !hasStart;
  for (const ch of chapters) {
    const blocks = [];
    for (const b of ch.blocks) {
      if (b.kind === "code") {
        if (started) blocks.push({ ...b });
        continue;
      }
      let text = b.text || "";
      if (!started) {
        const m = START.exec(text);
        if (!m) continue; // still in the front matter
        started = true;
        text = text.slice(m.index + m[0].length);
        const rest = END.exec(text);
        if (rest) text = text.slice(0, rest.index);
        if (b.kind !== "divider" && b.kind !== "image") {
          text = clean(text);
          if (!text) continue;
        }
      } else {
        const m = END.exec(text);
        if (m) {
          text = clean(text.slice(0, m.index));
          if (b.kind === "image") blocks.push(b);
          else if (text && b.kind !== "divider") blocks.push({ ...b, text });
          return finish(out, blocks, ch);
        }
        if (b.kind !== "divider" && b.kind !== "image" && END_LINE.test(text)) {
          return finish(out, blocks, ch);
        }
      }
      blocks.push({ ...b, text: b.kind === "divider" || b.kind === "image" ? "" : text });
    }
    ch.blocks = blocks;
    if (blocks.length > 0) out.push(ch);
  }
  return out;
}

/**
 * @param {Dalibre.Chapter[]} out
 * @param {Dalibre.Block[]} blocks
 * @param {Dalibre.Chapter} ch
 */
function finish(out, blocks, ch) {
  if (blocks.length > 0) out.push({ ...ch, blocks });
  return out;
}

/**
 * Short lines that repeat at the top or foot of chapters are running
 * headers, as a book's title or the part's name: they aren't text.
 * @param {Dalibre.Chapter[]} chapters
 */
function runningHeaders(chapters) {
  /** @type {Map<string, number>} */
  const count = new Map();
  const note = (b) => {
    const t = blockText(b);
    if (t && t.length <= 80) count.set(t.toLowerCase(), (count.get(t.toLowerCase()) || 0) + 1);
  };
  for (const ch of chapters) {
    ch.blocks.slice(0, 2).forEach(note);
    ch.blocks.slice(-2).forEach(note);
  }
  const headers = new Set();
  for (const [text, n] of count) if (n >= 3) headers.add(text);
  return headers;
}

/**
 * @param {Dalibre.Block[]} blocks
 * @param {Set<string>} headers
 */
function normalizeBlocks(blocks, headers) {
  /** @type {Dalibre.Block[]} */
  const out = [];
  for (const b of blocks) {
    if (b.kind === "image") {
      if (!b.src) continue;
      out.push({ kind: "image", src: b.src, alt: clean(b.alt), caption: clean(b.caption) });
      continue;
    }
    if (b.kind === "divider") {
      // A rule at the start or end has nothing to divide.
      if (out.length > 0) out.push({ kind: "divider", text: "" });
      continue;
    }
    if (b.kind === "code") {
      const text = codeText(b.text);
      if (text) out.push({ kind: "code", text });
      continue;
    }
    if (b.kind === "table") {
      const t = b.table;
      if (!t || (!t.caption && (!t.rows || t.rows.length === 0))) continue;
      out.push({ kind: "table", text: "", table: t });
      continue;
    }
    let text = blockText(b);
    if (!text) continue;
    if (headers.has(text.toLowerCase())) continue;
    if (/^\d{1,4}$/.test(text)) continue; // a page number left in the text
    if (b.kind === "heading") {
      if (text.length > 200) continue; // a "heading" that is a paragraph
      out.push({ kind: "heading", text, level: Math.min(Math.max(b.level || 2, 1), 6) });
      continue;
    }
    const prev = out[out.length - 1];
    if (prev && prev.kind === b.kind && prev.kind !== "heading" && joinable(prev.text, text)) {
      prev.text = join(prev.text, text);
      continue;
    }
    out.push({ kind: b.kind, text });
  }
  while (out.length > 0 && out[out.length - 1].kind === "divider") out.pop();
  return out;
}

/** A paragraph a converter split: the first has no end, the next goes on. */
function joinable(prev, next) {
  if (prev.length > 120) return false;
  if (/[.!?…。！？：:；;”"’')]$/.test(prev)) return false;
  if (!next) return false;
  if (/^[a-z0-9,;:)]/.test(next)) return true;
  return prev.length < 60 && !/[.!?…。！？：:；;”"’')]$/.test(prev);
}

/** @param {string} a @param {string} b */
function join(a, b) {
  if (/[\u2e80-\u9fff\u3040-\u30ff]$/.test(a) || /^[\u2e80-\u9fff\u3040-\u30ff]/.test(b)) return a + b;
  return a + " " + b;
}

/** The chapter's name is its first heading or short line, which then isn't text. */
function takeTitle(blocks) {
  for (let i = 0; i < Math.min(blocks.length, 3); i++) {
    const b = blocks[i];
    if (b.kind === "heading") {
      const t = blockText(b);
      blocks.splice(i, 1);
      return t;
    }
  }
  return "";
}
