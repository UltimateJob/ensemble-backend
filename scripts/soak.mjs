#!/usr/bin/env node
/**
 * 压力测试：连续跑很多轮，看长时间运行后才暴露的问题。
 *
 *   node scripts/soak.mjs                 # 默认 100 轮，mock 上游
 *   ROUNDS=20 LIVE=1 node scripts/soak.mjs  # 真实 DeepSeek
 *
 * 监控：进程内存、数据目录体积、消息总数、单轮耗时、额度。
 * 这些指标短跑看不出来，跑久了才会露出内存泄漏或文件膨胀。
 */

import { execSync } from 'node:child_process';
import { statSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const ROUNDS = +(process.env.ROUNDS || 100);
const LIVE = process.env.LIVE === '1';
const DATA = process.env.DATA_DIR || 'data';

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function api(path, { method = 'GET', token, body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: 'Bearer ' + token } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const j = await res.json().catch(() => ({}));
  if (!res.ok || j.ok === false) throw new Error((j.error && j.error.message) || res.status);
  return j.data;
}

function dirSize(dir) {
  try {
    let total = 0;
    const walk = d => {
      for (const name of readdirSync(d, { withFileTypes: true })) {
        const p = join(d, name.name);
        if (name.isDirectory()) walk(p);
        else {
          try { total += statSync(p).size; } catch { /* 忽略 */ }
        }
      }
    };
    walk(dir);
    return total;
  } catch {
    return 0;
  }
}

function rssMB() {
  try {
    const out = execSync("ps -o rss= -p $(pgrep -f 'bin/ensemble|/tmp/ens' | head -1)", {
      encoding: 'utf8',
    }).trim();
    return Math.round(+out / 1024);
  } catch {
    return null;
  }
}

const fmtKB = b => (b / 1024).toFixed(0) + ' KB';

(async () => {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) {
    console.error('后端没起来');
    process.exit(2);
  }

  const { token } = await api('/v1/auth/login', {
    method: 'POST', body: { kind: 'phone', target: '13800000000' },
  });
  const me0 = await api('/v1/me', { token });

  console.log(`\n压力测试：${ROUNDS} 轮 · ${LIVE ? '真实 DeepSeek' : 'mock 上游'}\n`);

  // 一个群，反复说话
  const conv = await api('/v1/conversations', {
    method: 'POST', token,
    body: { title: `压测 ${new Date().toISOString().slice(11, 19)}`, studio: 'companion' },
  });

  const samples = [];
  const t0 = Date.now();
  let errors = 0;

  for (let i = 1; i <= ROUNDS; i++) {
    const start = Date.now();
    let ok = true;
    try {
      const res = await fetch(`${BASE}/v1/conversations/${conv.id}/orchestrate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
        body: JSON.stringify({ content: `第 ${i} 轮：今天随便聊聊` }),
      });
      if (!res.ok) { ok = false; errors++; }
      // 读完整个流，确保连接释放
      await res.text();
    } catch {
      ok = false;
      errors++;
    }
    const ms = Date.now() - start;
    samples.push({ ms, ok });

    if (i % Math.max(1, Math.floor(ROUNDS / 10)) === 0 || i === ROUNDS) {
      const pump = await api('/v1/conversations/' + conv.id + '/messages?limit=1', { token })
        .then(d => (Array.isArray(d) ? d.length : d.messages.length))
        .catch(() => -1);
      const rss = rssMB();
      const size = dirSize(DATA);
      const recent = samples.slice(-10);
      const avg = Math.round(recent.reduce((a, s) => a + s.ms, 0) / recent.length);
      const me = await api('/v1/me', { token });
      console.log(
        `  ${String(i).padStart(3)}/${ROUNDS}  ` +
        `平均 ${String(avg).padStart(5)}ms  ` +
        `内存 ${rss == null ? '?' : rss + 'MB'}  ` +
        `数据 ${fmtKB(size).padStart(8)}  ` +
        `额度 ${me.user.used}/${me.user.quota}`
      );
      await sleep(150);
    }
  }

  const total = Date.now() - t0;
  const me1 = await api('/v1/me', { token });
  const msgs = await api('/v1/conversations/' + conv.id + '/messages?limit=200', { token });
  const list = Array.isArray(msgs) ? msgs : msgs.messages;

  // 一致性检查：消息序号必须严格递增（同一秒也可能写入多条）
  let order = true;
  let last = '';
  for (const m of list) {
    if (m.createdAt < last) { order = false; break; }
    last = m.createdAt;
  }

  // 序列检查：用户消息必须都落库
  const userMsgs = list.filter(m => m.role === 'user').length;

  console.log('\n─'.repeat(52));
  console.log(`总耗时        ${(total / 1000).toFixed(1)}s`);
  console.log(`失败轮次      ${errors}`);
  console.log(`额度变化      ${me0.user.used} → ${me1.user.used}（增加 ${me1.user.used - me0.user.used}）`);
  console.log(`消息总数      ${list.length}（用户 ${userMsgs} 条）`);
  console.log(`数据目录      ${fmtKB(dirSize(DATA))}`);
  console.log(`消息顺序      ${order ? '正常' : '异常（有倒序）'}`);
  console.log('─'.repeat(52));

  // 清理
  await fetch(`${BASE}/v1/conversations/${conv.id}`, {
    method: 'DELETE', headers: { Authorization: 'Bearer ' + token },
  }).catch(() => {});

  const problems = [];
  if (errors > 0) problems.push(`有 ${errors} 轮失败`);
  if (userMsgs !== ROUNDS) problems.push(`用户消息应 ${ROUNDS} 条，实际 ${userMsgs} 条`);
  if (!order) problems.push('消息时间出现倒序');

  if (problems.length) {
    console.log('\n发现的问题：');
    problems.forEach(p => console.log('  ! ' + p));
    process.exit(1);
  }
  console.log('\n压测通过\n');
})();
