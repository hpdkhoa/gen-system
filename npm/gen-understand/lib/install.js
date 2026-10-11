'use strict';
// Finds or downloads the understand-mcp binary that matches this package's version.
//
// The binary comes from the gen-system GitHub release for this version. The
// download is checked against the release's checksums.txt before it is used, and
// cached, so later runs start at once and work offline. Everything here logs to
// stderr: stdout belongs to the MCP protocol.

const crypto = require('node:crypto');
const fs = require('node:fs');
const https = require('node:https');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');

const VERSION = require('../package.json').version;
const REPO = 'https://github.com/hpdkhoa/gen-system';

// GOOS and GOARCH names for this machine, or an error naming what is missing.
function target(platform = process.platform, arch = process.arch) {
  const goos = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[platform];
  const goarch = { x64: 'amd64', arm64: 'arm64' }[arch];
  if (!goos || !goarch) {
    throw new Error(`no understand-mcp build for ${platform}/${arch}. ` +
      'Build it from source (go install github.com/hpdkhoa/gen-system/generator/cmd/understand-mcp@latest) ' +
      'and set UNDERSTAND_MCP_BINARY to its path.');
  }
  return { goos, goarch };
}

// The release asset name, as the release workflow writes it.
function assetName(t) {
  return `understand-mcp-${t.goos}-${t.goarch}${t.goos === 'windows' ? '.exe' : ''}`;
}

function downloadBase(version = VERSION) {
  return process.env.UNDERSTAND_MCP_DOWNLOAD_BASE || `${REPO}/releases/download/understand-mcp-v${version}`;
}

function cacheDir(version = VERSION) {
  if (process.env.UNDERSTAND_MCP_CACHE) return path.join(process.env.UNDERSTAND_MCP_CACHE, version);
  const base = process.platform === 'win32'
    ? (process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData', 'Local'))
    : (process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache'));
  return path.join(base, 'gen-understand', version);
}

// checksums.txt lines read "<sha256>  <file name>", as sha256sum writes them.
function parseChecksums(text) {
  const out = {};
  for (const line of text.split(/\r?\n/)) {
    const m = line.trim().match(/^([0-9a-f]{64})\s+\*?(\S+)$/i);
    if (m) out[m[2]] = m[1].toLowerCase();
  }
  return out;
}

// GET a URL into a Buffer, following redirects (GitHub sends release assets
// through one).
function fetchBuffer(url, redirects = 5) {
  return new Promise((resolve, reject) => {
    const lib = url.startsWith('https:') ? https : http;
    const req = lib.get(url, { headers: { 'User-Agent': 'gen-understand' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        if (redirects <= 0) return reject(new Error(`too many redirects at ${url}`));
        return resolve(fetchBuffer(new URL(res.headers.location, url).toString(), redirects - 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`download failed: ${url} answered ${res.statusCode}`));
      }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    });
    req.setTimeout(120000, () => req.destroy(new Error(`download timed out: ${url}`)));
    req.on('error', reject);
  });
}

// Returns the path of a ready binary, downloading it on first use.
async function ensureBinary({ version = VERSION, log = (m) => process.stderr.write(m + '\n') } = {}) {
  if (process.env.UNDERSTAND_MCP_BINARY) {
    const p = process.env.UNDERSTAND_MCP_BINARY;
    if (!fs.existsSync(p)) throw new Error(`UNDERSTAND_MCP_BINARY points at ${p}, which does not exist`);
    return p;
  }
  const t = target();
  const name = assetName(t);
  const dir = cacheDir(version);
  const bin = path.join(dir, name);
  if (fs.existsSync(bin)) return bin;

  const base = downloadBase(version);
  log(`gen-understand: downloading ${name} ${version} from ${base}`);
  const sums = parseChecksums((await fetchBuffer(`${base}/checksums.txt`)).toString('utf8'));
  const want = sums[name];
  if (!want) throw new Error(`checksums.txt of ${version} has no entry for ${name}`);
  const data = await fetchBuffer(`${base}/${name}`);
  const got = crypto.createHash('sha256').update(data).digest('hex');
  if (got !== want) {
    throw new Error(`checksum mismatch for ${name}: expected ${want}, got ${got}. The file was not used.`);
  }
  fs.mkdirSync(dir, { recursive: true });
  // Write beside the target, then rename, so a second process never runs half a file.
  const tmp = `${bin}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, data, { mode: 0o755 });
  fs.renameSync(tmp, bin);
  log(`gen-understand: cached at ${bin}`);
  return bin;
}

module.exports = { VERSION, target, assetName, downloadBase, cacheDir, parseChecksums, fetchBuffer, ensureBinary };
