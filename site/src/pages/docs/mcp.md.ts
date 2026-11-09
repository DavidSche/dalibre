import type { APIRoute } from "astro";
import { getEntry } from "astro:content";

// The MCP docs as plain Markdown, for LLMs and other tools.
export const GET: APIRoute = async () => {
  const entry = await getEntry("docs", "mcp");
  if (!entry) return new Response("Not found", { status: 404 });
  const body = `# Dalibre MCP\n\n> ${entry.data.description}\n\n${entry.body ?? ""}`;
  return new Response(body, { headers: { "Content-Type": "text/markdown; charset=utf-8" } });
};
