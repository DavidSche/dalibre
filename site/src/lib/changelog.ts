// The release notes, read from the repository's CHANGELOG.md when the site
// builds: the same notes Dalibre's update window shows.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export interface Version {
  version: string;
  /** Each note, as HTML. */
  notes: string[];
}

// The site builds from site/, next to the repository's root.
const source = readFileSync(resolve(process.cwd(), "../CHANGELOG.md"), "utf8");

const escape = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** The Markdown the notes use inline: `code`, **bold**, *em* and links. */
function inline(text: string): string {
  const codes: string[] = [];
  let s = escape(text).replace(/`([^`]+)`/g, (_, c) => `\u0000${codes.push(c) - 1}\u0000`);
  s = s
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/(^|[^*])\*([^*]+)\*/g, "$1<em>$2</em>")
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, t, u) => `<a href="${u}"${/^https?:/.test(u) ? ' target="_blank" rel="noopener"' : ""}>${t}</a>`);
  return s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${codes[+i]}</code>`);
}

function parse(md: string): Version[] {
  const versions: Version[] = [];
  let cur: Version | undefined;
  let item: string[] | undefined;
  const flush = () => {
    if (cur && item) cur.notes.push(inline(item.join(" ")));
    item = undefined;
  };
  for (const line of md.split("\n")) {
    const head = line.match(/^## (.+)$/);
    if (head) {
      flush();
      cur = { version: head[1].trim(), notes: [] };
      versions.push(cur);
    } else if (!cur) {
      continue; // the file's own preamble
    } else if (line.startsWith("- ")) {
      flush();
      item = [line.slice(2).trim()];
    } else if (line.trim() === "") {
      flush();
    } else if (item) {
      item.push(line.trim());
    } else {
      item = [line.trim()];
    }
  }
  flush();
  return versions;
}

export const changelog = parse(source);
