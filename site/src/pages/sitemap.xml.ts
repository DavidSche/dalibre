import type { APIRoute } from "astro";
import { getCollection } from "astro:content";
import { href, slugOf } from "../lib/docs-nav";

export const GET: APIRoute = async ({ site }) => {
  const docs = await getCollection("docs");
  const paths = ["/", "/changelog/", ...docs.map((d) => href(slugOf(d.id)))];
  const urls = paths.map((p) => `  <url><loc>${new URL(p, site)}</loc></url>`).join("\n");
  return new Response(
    `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls}\n</urlset>\n`,
    { headers: { "Content-Type": "application/xml; charset=utf-8" } },
  );
};
