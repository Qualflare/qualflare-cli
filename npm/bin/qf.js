#!/usr/bin/env node
'use strict';

// Thin launcher: exec the platform-native `qf` binary that install.js placed next to
// this file, forwarding args, stdio, and the exit code.

const path = require('node:path');
const { spawnSync } = require('node:child_process');

const bin = path.join(__dirname, process.platform === 'win32' ? 'qf.exe' : 'qf');
const res = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });

if (res.error) {
  console.error(`[qualflare] could not run qf: ${res.error.message}`);
  console.error('[qualflare] try reinstalling: npm install -g @qualflare/cli');
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
