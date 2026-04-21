#!/usr/bin/env node
// lambit Node.js runner shim
// Usage: node runner.mjs <handler> <json-payload>
//
// handler format: "<file>.<export>"
// e.g. "index.handler" -> import('../../../index.js').then(m => m.handler(event, ctx))

import { createRequire } from 'module';
import path from 'path';
import { fileURLToPath } from 'url';

const [,, handlerArg, payloadArg] = process.argv;

if (!handlerArg) {
  console.error('Usage: runner.mjs <handler> <json-payload>');
  process.exit(1);
}

const payload = payloadArg || '{}';
let event;
try {
  event = JSON.parse(payload);
} catch (e) {
  console.error('Invalid JSON payload:', e.message);
  process.exit(1);
}

// Resolve handler: "file.export" or "file/sub.export"
const lastDot = handlerArg.lastIndexOf('.');
if (lastDot < 0) {
  console.error('Handler must be in format <file>.<export>, got:', handlerArg);
  process.exit(1);
}
const filePart = handlerArg.slice(0, lastDot);
const exportPart = handlerArg.slice(lastDot + 1);

// The project root is three levels up from .lambit/node-runner/runner.mjs
const shimDir = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(shimDir, '..', '..', '..');

// Try .mjs, .js extensions.
const candidates = [
  path.join(projectRoot, filePart + '.mjs'),
  path.join(projectRoot, filePart + '.js'),
  path.join(projectRoot, filePart),
];

let mod;
for (const candidate of candidates) {
  try {
    mod = await import(candidate);
    break;
  } catch (e) {
    if (e.code !== 'ERR_MODULE_NOT_FOUND') {
      console.error('Error loading module:', e.message);
      process.exit(1);
    }
  }
}

if (!mod) {
  // Fall back to CommonJS require.
  const require = createRequire(import.meta.url);
  for (const candidate of candidates) {
    try {
      mod = require(candidate);
      break;
    } catch (e) {
      if (e.code !== 'MODULE_NOT_FOUND') {
        console.error('Error loading module:', e.message);
        process.exit(1);
      }
    }
  }
}

if (!mod) {
  console.error('Could not find module:', filePart, '(tried', candidates.join(', ') + ')');
  process.exit(1);
}

const fn = mod[exportPart] || mod.default?.[exportPart];
if (typeof fn !== 'function') {
  console.error('Export', exportPart, 'is not a function in', filePart);
  process.exit(1);
}

// Minimal Lambda context stub.
const context = {
  functionName: exportPart,
  functionVersion: '$LATEST',
  invokedFunctionArn: 'arn:aws:lambda:local:000000000000:function:local',
  memoryLimitInMB: '128',
  awsRequestId: 'lambit-local-' + Date.now(),
  logGroupName: '/aws/lambda/local',
  logStreamName: 'lambit',
  getRemainingTimeInMillis: () => 30000,
  done: () => {},
  fail: () => {},
  succeed: () => {},
};

try {
  const result = await fn(event, context);
  console.log(JSON.stringify(result, null, 2));
} catch (e) {
  console.error(e.message || String(e));
  process.exit(1);
}
