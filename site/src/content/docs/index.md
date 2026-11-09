---
title: Dalibre docs
description: Guides and references for Dalibre, the lightweight e-book manager for calibre libraries, and for the plugins that extend it.
---

Dalibre is a calm, fast desktop app for your e-books. It opens the calibre
library you already have, in a single native binary, and grows with
plugins anyone can write.

These docs are for people who want to connect it to their agents or
extend it. More guides for using the app will follow.

## MCP

Your own AI agents, such as Claude Code, Claude Desktop and Cursor, can
use your library over MCP: search it, edit metadata, add books, and
convert formats with their own tools.

[**MCP**](/docs/mcp/) has the setup for each agent, examples of what to
ask, and every tool. As plain Markdown: [`/docs/mcp.md`](/docs/mcp.md).

## Plugins

A plugin is a folder with a `plugin.json` and a JavaScript file. It
teaches Dalibre where to find books, or how to look them up, and never
draws UI of its own.

[**Writing plugins**](/docs/plugins/) has everything on one page: an
overview, a quick start that builds a working book source, and the full
reference for `plugin.json`, book sources, metadata sources, the runtime,
permissions, the CLI, publishing and every type.

Working with an AI assistant? Give it
[`/docs/plugins.md`](/docs/plugins.md), the same page as plain Markdown,
or point it at [`/llms.txt`](/llms.txt).

## calibre compatibility

Dalibre reads and writes calibre's own library format: `metadata.db`,
folder and file names, and `metadata.opf` next to each book. You can
open the same library in either app, any day. Nothing to import, nothing
to export.
