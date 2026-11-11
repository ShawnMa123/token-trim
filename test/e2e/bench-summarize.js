#!/usr/bin/env node
// Render bench.json as Markdown and compute wrapper overhead / byte reduction.
const fs = require('fs');
const path = require('path');
const file = process.argv[2] || path.join(__dirname, 'results', 'bench.json');
const cases = JSON.parse(fs.readFileSync(file, 'utf8'));

console.log('| case | arm | p50 ms | p95 ms | max RSS MiB | stdout bytes | byte reduction |');
console.log('|---|---|---:|---:|---:|---:|---:|');
for (const c of cases) {
  const direct = c.arms.find((a) => a.arm === 'direct');
  for (const a of c.arms) {
    const reduction = direct && direct.stdout_bytes
      ? ((1 - a.stdout_bytes / direct.stdout_bytes) * 100).toFixed(1) + '%'
      : '-';
    console.log(
      `| ${c.case} | ${a.arm} | ${a.p50_ms.toFixed(1)} | ${a.p95_ms.toFixed(1)} | ` +
      `${(a.max_rss_kb / 1024).toFixed(1)} | ${a.stdout_bytes} | ${reduction} |`
    );
  }
}

console.log('\nWrapper overhead (p95, this run):');
for (const c of cases) {
  const direct = c.arms.find((a) => a.arm === 'direct');
  for (const a of c.arms.filter((x) => x.arm !== 'direct')) {
    console.log(
      `- ${c.case}/${a.arm}: +${(a.p95_ms - direct.p95_ms).toFixed(1)} ms p95, ` +
      `RSS ${(a.max_rss_kb / 1024).toFixed(1)} MiB (direct ${(direct.max_rss_kb / 1024).toFixed(1)} MiB)`
    );
  }
}
