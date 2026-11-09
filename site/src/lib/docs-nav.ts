// The docs sidebar, in reading order. `slug` is the page's path under /docs.
export interface NavItem { slug: string; title: string }
export interface NavGroup { title: string; items: NavItem[] }

export const nav: NavGroup[] = [
  {
    title: "Start",
    items: [{ slug: "", title: "Introduction" }],
  },
  {
    title: "Agents",
    items: [{ slug: "mcp", title: "MCP" }],
  },
  {
    title: "Plugins",
    items: [{ slug: "plugins", title: "Writing plugins" }],
  },
];

export const flat = nav.flatMap((g) => g.items);

export const href = (slug: string) => (slug ? `/docs/${slug}/` : "/docs/");

/** The page's slug from a collection entry id: "plugins/index" → "plugins". */
export const slugOf = (id: string) => id.replace(/(^|\/)index$/, "");
