#!/usr/bin/env node
// Score long-log/long-text runs and report token/byte usage.
const fs = require('fs');
const path = require('path');

const dir = process.argv[2] || path.join(__dirname, 'results-longlog');
const files = fs.readdirSync(dir).filter((f) => /^\d+-(baseline|lossless)\.jsonl$/.test(f)).sort();
const runs = [];
for (const file of files) {
  const m = file.match(/^(\d+)-(baseline|lossless)\.jsonl$/);
  const lines = fs.readFileSync(path.join(dir, file), 'utf8').split('\n').filter(Boolean);
  const usage = { input_tokens: 0, cached_input_tokens: 0, output_tokens: 0 };
  const commands = [];
  const messages = [];
  let completed = false;
  for (const line of lines) {
    let ev; try { ev = JSON.parse(line); } catch { continue; }
    if (ev.type === 'turn.completed') {
      completed = true;
      for (const k of Object.keys(usage)) usage[k] += (ev.usage && ev.usage[k]) || 0;
    }
    if (ev.type === 'item.completed' && ev.item) {
      if (ev.item.type === 'command_execution') {
        commands.push({ command: ev.item.command || '', outputBytes: Buffer.byteLength(ev.item.aggregated_output || '') });
      }
      if (ev.item.type === 'agent_message') messages.push(ev.item.text || '');
    }
  }
  const last = (messages.at(-1) || '').trim();
  let answer = null;
  const brace = last.indexOf('{');
  if (brace >= 0) {
    const end = last.lastIndexOf('}');
    if (end > brace) { try { answer = JSON.parse(last.slice(brace, end + 1)); } catch {} }
  }
  const a = answer || {};
  const ok =
    Number(a.error_count) === 3 &&
    String(a.fatal_request_id || '') === 'REQ-7391' &&
    String(a.second_stack_frame || '').includes('server.go:88') &&
    String(a.audit_window || '').includes('03:17');
  runs.push({ file, pair: Number(m[1]), arm: m[2], completed, ...usage,
    outputBytes: commands.reduce((n, c) => n + c.outputBytes, 0),
    commands, last, answer, ok });
}

const med = (xs) => {
  if (!xs.length) return 0;
  const s = [...xs].sort((a, b) => a - b), mid = Math.floor(s.length / 2);
  return s.length % 2 ? s[mid] : (s[mid - 1] + s[mid]) / 2;
};
console.log('file                         arm      input  cached  output  outBytes cmds done correct');
for (const r of runs) {
  console.log(r.file.padEnd(28), r.arm.padEnd(8),
    String(r.input_tokens).padStart(6), String(r.cached_input_tokens).padStart(7),
    String(r.output_tokens).padStart(7), String(r.outputBytes).padStart(9),
    String(r.commands.length).padStart(4), String(r.completed).padStart(5), String(r.ok).padStart(7));
  console.log('  answer:', JSON.stringify(r.answer));
}
for (const arm of ['baseline', 'lossless']) {
  const rs = runs.filter((r) => r.arm === arm && r.completed);
  if (!rs.length) { console.log(arm, 'no valid runs'); continue; }
  console.log(arm, 'input median', med(rs.map((r) => r.input_tokens)),
    'bytes median', med(rs.map((r) => r.outputBytes)),
    'correct', rs.filter((r) => r.ok).length + '/' + rs.length);
}
