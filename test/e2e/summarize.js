#!/usr/bin/env node
// Summarize Codex JSONL runs for baseline / rtk-go / rtk and compute savings.
const fs = require('fs');
const path = require('path');

const dir = process.argv[2] || path.join(__dirname, 'results');
const expectedCommands = Number(process.argv[3] || 3);
const files = fs.readdirSync(dir).filter((f) => f.endsWith('.jsonl')).sort();
const ARMS = ['baseline', 'rtk-go', 'rtk'];
const runs = [];

for (const file of files) {
  const m = file.match(/^(\d+)-(baseline|rtk-go|rtk)\.jsonl$/);
  if (!m) continue;
  const lines = fs.readFileSync(path.join(dir, file), 'utf8').split('\n').filter(Boolean);
  const usage = { input_tokens: 0, cached_input_tokens: 0, output_tokens: 0 };
  const commands = [];
  const messages = [];
  let completed = false;
  for (const line of lines) {
    let ev;
    try { ev = JSON.parse(line); } catch { continue; }
    if (ev.type === 'turn.completed') {
      completed = true;
      for (const k of Object.keys(usage)) usage[k] += (ev.usage && ev.usage[k]) || 0;
    }
    if (ev.type === 'item.completed' && ev.item) {
      if (ev.item.type === 'command_execution') {
        commands.push({
          command: ev.item.command || '',
          outputBytes: Buffer.byteLength(ev.item.aggregated_output || ''),
          exitCode: ev.item.exit_code,
        });
      }
      if (ev.item.type === 'agent_message') messages.push(ev.item.text || '');
    }
  }
  const outputBytes = commands.reduce((a, c) => a + c.outputBytes, 0);
  runs.push({
    file, pair: Number(m[1]), arm: m[2], completed, ...usage,
    outputBytes, commands, lastMessage: messages.at(-1) || '',
  });
}

const median = (xs) => {
  if (!xs.length) return 0;
  const s = [...xs].sort((a, b) => a - b);
  const mid = Math.floor(s.length / 2);
  return s.length % 2 ? s[mid] : (s[mid - 1] + s[mid]) / 2;
};
const mean = (xs) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 0);

console.log('file                           arm       input   cached  output  outBytes  cmds done');
for (const r of runs) {
  console.log(
    r.file.padEnd(30),
    r.arm.padEnd(9),
    String(r.input_tokens).padStart(7),
    String(r.cached_input_tokens).padStart(8),
    String(r.output_tokens).padStart(7),
    String(r.outputBytes).padStart(9),
    String(r.commands.length).padStart(5),
    String(r.completed).padStart(5)
  );
}

const valid = runs.filter((r) => r.completed && r.commands.length === expectedCommands && r.lastMessage.trim() === 'DONE');
const byArm = (arm) => valid.filter((r) => r.arm === arm);
const baseline = byArm('baseline');
if (!baseline.length) {
  console.error(`\nNo valid baseline runs (valid=${valid.length}, total=${runs.length})`);
  process.exit(1);
}
const bIn = baseline.map((r) => r.input_tokens);
const bMed = median(bIn);
const bMean = mean(bIn);
const bOutBytes = median(baseline.map((r) => r.outputBytes));

console.log('\nvalid runs: %s', ARMS.map((a) => `${a}=${byArm(a).length}`).join(' '));
console.log(`baseline: input median=${bMed} mean=${bMean.toFixed(1)}, tool-output median=${bOutBytes} bytes`);
for (const arm of ARMS.filter((a) => a !== 'baseline')) {
  const rs = byArm(arm);
  if (!rs.length) { console.log('%s: no valid runs', arm); continue; }
  const xs = rs.map((r) => r.input_tokens);
  const med = median(xs);
  const outB = median(rs.map((r) => r.outputBytes));
  console.log(
    `${arm}: input median=${med} mean=${mean(xs).toFixed(1)}, tool-output median=${outB} bytes | ` +
    `median reduction=${((1 - med / bMed) * 100).toFixed(1)}% bytes reduction=${(bOutBytes ? (1 - outB / bOutBytes) * 100 : 0).toFixed(1)}%`
  );
}
for (const r of runs) {
  const wrapped = r.commands.filter((c) => /rtk-go exec --|rtk (git|rg|go)/.test(c.command)).length;
  const byCmd = r.commands.map((c) => c.outputBytes).join('/');
  console.log(`  pair ${r.pair} ${r.arm.padEnd(9)} wrapped=${wrapped} bytes=${byCmd} last=${JSON.stringify(r.lastMessage.slice(0, 20))}`);
}
