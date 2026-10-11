#!/usr/bin/env node
'use strict';
// Starts the gen-system Understand MCP server. Arguments pass straight through,
// for example: npx -y gen-understand --root /path/to/repo

const { spawn } = require('node:child_process');
const { ensureBinary } = require('../lib/install.js');

ensureBinary().then((bin) => {
  const child = spawn(bin, process.argv.slice(2), { stdio: 'inherit', windowsHide: true });
  for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
    process.on(sig, () => { if (!child.killed) child.kill(sig); });
  }
  child.on('error', (err) => {
    process.stderr.write(`gen-understand: could not start ${bin}: ${err.message}\n`);
    process.exit(1);
  });
  child.on('exit', (code, signal) => {
    if (signal) process.kill(process.pid, signal);
    else process.exit(code ?? 1);
  });
}).catch((err) => {
  process.stderr.write(`gen-understand: ${err.message}\n`);
  process.exit(1);
});
