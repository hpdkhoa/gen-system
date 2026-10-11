'use strict';
const test = require('node:test');
const assert = require('node:assert');
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');

const inst = require('../lib/install.js');

test('target maps Node names to Go names', () => {
  assert.deepStrictEqual(inst.target('linux', 'x64'), { goos: 'linux', goarch: 'amd64' });
  assert.deepStrictEqual(inst.target('darwin', 'arm64'), { goos: 'darwin', goarch: 'arm64' });
  assert.deepStrictEqual(inst.target('win32', 'x64'), { goos: 'windows', goarch: 'amd64' });
  assert.throws(() => inst.target('aix', 'ppc64'), /no understand-mcp build for aix\/ppc64/);
});

test('asset names match the release workflow', () => {
  assert.strictEqual(inst.assetName({ goos: 'linux', goarch: 'amd64' }), 'understand-mcp-linux-amd64');
  assert.strictEqual(inst.assetName({ goos: 'windows', goarch: 'arm64' }), 'understand-mcp-windows-arm64.exe');
});

test('checksums parse the sha256sum format', () => {
  const a = 'a'.repeat(64), b = 'B'.repeat(64);
  const sums = inst.parseChecksums(`${a}  understand-mcp-linux-amd64\n${b} *understand-mcp-windows-amd64.exe\n\njunk\n`);
  assert.strictEqual(sums['understand-mcp-linux-amd64'], a);
  assert.strictEqual(sums['understand-mcp-windows-amd64.exe'], 'b'.repeat(64));
  assert.strictEqual(Object.keys(sums).length, 2);
});

// A fake release: /rel/checksums.txt, and the asset behind a redirect, as on GitHub.
function fakeRelease(assetBody, listedHash) {
  const name = inst.assetName(inst.target());
  const hash = listedHash || crypto.createHash('sha256').update(assetBody).digest('hex');
  const hits = { asset: 0 };
  const server = http.createServer((req, res) => {
    if (req.url === '/rel/checksums.txt') return res.end(`${hash}  ${name}\n`);
    if (req.url === `/rel/${name}`) {
      res.writeHead(302, { Location: `/objects/${name}` });
      return res.end();
    }
    if (req.url === `/objects/${name}`) {
      hits.asset++;
      return res.end(assetBody);
    }
    res.writeHead(404);
    res.end();
  });
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () =>
    resolve({ server, hits, base: `http://127.0.0.1:${server.address().port}/rel`, name })));
}

function withEnv(vars, fn) {
  const old = {};
  for (const k of Object.keys(vars)) { old[k] = process.env[k]; process.env[k] = vars[k]; }
  return Promise.resolve(fn()).finally(() => {
    for (const k of Object.keys(vars)) {
      if (old[k] === undefined) delete process.env[k]; else process.env[k] = old[k];
    }
  });
}

test('downloads, checks and caches the binary', async () => {
  const body = Buffer.from('#!/bin/sh\necho fake\n');
  const rel = await fakeRelease(body);
  const cache = fs.mkdtempSync(path.join(os.tmpdir(), 'gsu-'));
  try {
    await withEnv({ UNDERSTAND_MCP_DOWNLOAD_BASE: rel.base, UNDERSTAND_MCP_CACHE: cache, UNDERSTAND_MCP_BINARY: '' }, async () => {
      delete process.env.UNDERSTAND_MCP_BINARY;
      const logs = [];
      const bin = await inst.ensureBinary({ version: '9.9.9', log: (m) => logs.push(m) });
      assert.strictEqual(bin, path.join(cache, '9.9.9', rel.name));
      assert.deepStrictEqual(fs.readFileSync(bin), body);
      assert.ok(logs.some((m) => m.includes('downloading')));
      // second run: from the cache, no download
      await inst.ensureBinary({ version: '9.9.9', log: () => {} });
      assert.strictEqual(rel.hits.asset, 1);
    });
  } finally {
    rel.server.close();
    fs.rmSync(cache, { recursive: true, force: true });
  }
});

test('a checksum mismatch is refused and nothing is cached', async () => {
  const rel = await fakeRelease(Buffer.from('tampered'), 'c'.repeat(64));
  const cache = fs.mkdtempSync(path.join(os.tmpdir(), 'gsu-'));
  try {
    await withEnv({ UNDERSTAND_MCP_DOWNLOAD_BASE: rel.base, UNDERSTAND_MCP_CACHE: cache }, async () => {
      delete process.env.UNDERSTAND_MCP_BINARY;
      await assert.rejects(inst.ensureBinary({ version: '9.9.8', log: () => {} }), /checksum mismatch/);
      assert.strictEqual(fs.existsSync(path.join(cache, '9.9.8', rel.name)), false);
    });
  } finally {
    rel.server.close();
    fs.rmSync(cache, { recursive: true, force: true });
  }
});

test('UNDERSTAND_MCP_BINARY skips the download', async () => {
  const f = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'gsu-')), 'local-bin');
  fs.writeFileSync(f, 'x');
  await withEnv({ UNDERSTAND_MCP_BINARY: f, UNDERSTAND_MCP_DOWNLOAD_BASE: 'http://127.0.0.1:1/never' }, async () => {
    assert.strictEqual(await inst.ensureBinary({ log: () => {} }), f);
  });
  await withEnv({ UNDERSTAND_MCP_BINARY: f + '-missing' }, async () => {
    await assert.rejects(inst.ensureBinary({ log: () => {} }), /does not exist/);
  });
});
