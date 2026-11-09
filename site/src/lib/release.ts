// The latest Dalibre release, read from GitHub when the site builds, so the
// download links, version and sizes follow whatever was released last.

const repo = "DavidSche/dalibre";

export interface Download {
  id: string;
  label: string;
  os: string;
  url: string;
  size: string;
}

interface Asset {
  name: string;
  size: number;
  browser_download_url: string;
}

const platforms: { id: string; label: string; match: RegExp }[] = [
  { id: "mac-arm64", label: "macOS (Apple silicon)", match: /-arm64\.dmg$/ },
  { id: "mac-amd64", label: "macOS (Intel)", match: /-amd64\.dmg$/ },
  { id: "win-amd64", label: "Windows", match: /^dalibre-setup-.*-amd64\.exe$/ },
  { id: "win-arm64", label: "Windows (Arm)", match: /^dalibre-setup-.*-arm64\.exe$/ },
  { id: "linux-amd64", label: "Linux (.deb)", match: /_amd64\.deb$/ },
  { id: "linux-arm64", label: "Linux (Arm .deb)", match: /_arm64\.deb$/ },
];

function formatSize(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

async function load() {
  const headers: Record<string, string> = { Accept: "application/vnd.github+json" };
  const token = process.env.GITHUB_TOKEN ?? process.env.GH_TOKEN;
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`https://api.github.com/repos/${repo}/releases/latest`, { headers });
  if (!res.ok) throw new Error(`fetching the latest release of ${repo}: ${res.status} ${res.statusText}`);
  const release = (await res.json()) as { tag_name: string; html_url: string; assets: Asset[] };

  const downloads: Download[] = [];
  for (const p of platforms) {
    const asset = release.assets.find((a) => p.match.test(a.name));
    if (asset) downloads.push({ id: p.id, label: p.label, os: p.id.startsWith("mac") ? "macOS" : p.id.startsWith("win") ? "Windows" : "Linux", url: asset.browser_download_url, size: formatSize(asset.size) });
  }
  if (!downloads.length) throw new Error(`the latest release of ${repo} has no installers`);
  return { version: release.tag_name.replace(/^v/, ""), page: release.html_url, downloads };
}

export const release = await load();
