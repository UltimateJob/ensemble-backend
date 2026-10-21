#!/usr/bin/env node
/**
 * 并发压测：多个用户同时用。
 *
 *   node scripts/concurrent.mjs                 # 12 用户 × 3 轮，mock
 *   USERS=6 ROUNDS=2 LIVE=1 node scripts/concurrent.mjs
 *
 * 之前的压测都是串行发请求，验证的是「跑得久会不会坏」。
 * 这个验证的是「一群人同时用会不会串」：
 *   - 跨用户数据是否隔离（A 的群不能出现 B 的消息）
 *   - 同一用户多标签页同时发言，是否被会话锁挡住
 *   - 额度在并发下会不会丢更新
 *   - 有没有 5xx
 */

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const USERS = +(process.env.USERS || 12);
const ROUNDS = +(process.env.ROUNDS || 3);
const LIVE = process.env.LIVE === '1';

const sleep = ms => new Promise(r => setTimeout(r, ms));
let pass = 0, fail = 0;
const problems = [];
const ok = (name, cond, detail) => {
  if (cond) { pass++; console.log(`  \x1b[32m✓\x1b[0m ${name}`); }
  else { fail++; problems.push(name); console.log(`  \x1b[31m✗\x1b[0m ${name}${detail ? '  → ' + detail : ''}`); }
};

async function api(path, { method = 'GET', token, body, expectFail = false } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: 'Bearer ' + token } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const j = await res.json().catch(() => ({}));
  if (!res.ok || j.ok === false) {
    if (expectFail) return { failed: true, status: res.status };
    const e = new Error((j.error && j.error.message) || ('HTTP ' + res.status));
    e.status = res.status;
    throw e;
  }
  return j.data;
}

/** 注册并登录一个新用户（走验证码流程） */
async function newUser(i) {
  const phone = '139' + String(10000000 + i).slice(0, 8);
  let code = '123456';
  try {
    const r = await api('/v1/auth/send-code', {
      method: 'POST', body: { kind: 'phone', target: phone },
    });
    code = r.devCode || code;
  } catch { /* 通道不同则用固定码 */ }
  const d = await api('/v1/auth/login', {
    method: 'POST', body: { kind: 'phone', target: phone, code },
  });
  return { phone, token: d.token, user: d.user };
}

/** 发一轮对话，读完 SSE */
async function speak(token, convId, text) {
  const res = await fetch(`${BASE}/v1/conversations/${convId}/orchestrate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ content: text }),
  });
  if (!res.ok) return { status: res.status, busy: false };
  const body = await res.text();
  const busy = body.includes('"type":"busy"');
  const ends = (body.match(/"type":"end"/g) || []).length;
  return { status: res.status, busy, ends };
}

(async () => {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来'); process.exit(2); }

  console.log(`\n并发压测：${USERS} 用户 × ${ROUNDS} 轮 · ${LIVE ? '真实 Key' : 'mock'}\n`);

  const t0 = Date.now();

  // ---- 1. 并发注册登录 ----
  const users = await Promise.all(
    Array.from({ length: USERS }, (_, i) => newUser(i).catch(e => ({ error: e.message })))
  );
  const bad = users.filter(u => u.error);
  ok(`${USERS} 个用户并发注册登录`, bad.length === 0, bad.map(b => b.error).join(', '));
  const alive = users.filter(u => !u.error);
  if (!alive.length) process.exit(1);

  // 记下每个人压测开始前的额度基线——used 是全局累计，
  // 不能直接跟单个群的消息数比。
  const baseline = await Promise.all(
    alive.map(u => api('/v1/me', { token: u.token }).then(d => d.user.used).catch(() => 0))
  );

  // ---- 2. 并发建群 ----
  const convs = await Promise.all(
    alive.map(u => api('/v1/conversations', {
      method: 'POST', token: u.token,
      body: { title: '并发群 ' + u.phone.slice(-4), studio: 'companion' },
    }).catch(e => ({ error: e.message })))
  );
  ok('并发建群全部成功', convs.every(c => !c.error));

  // ---- 3. 并发发言（跨用户）----
  const speakResults = await Promise.all(
    alive.map((u, i) => speak(u.token, convs[i].id, `我是 ${u.phone.slice(-4)}，第 1 轮`))
  );
  const httpErrors = speakResults.filter(r => r.status >= 500);
  ok('没有 5xx', httpErrors.length === 0, httpErrors.map(r => r.status).join(','));
  ok('绝大多数请求被接受', speakResults.filter(r => r.status === 200).length >= alive.length * 0.8,
     speakResults.map(r => r.status).join(','));

  // ---- 4. 跨用户隔离 ----
  // 每个人只能看到自己的群。
  // 注意：测试手机号是复用的，上一次跑残留的群还在，所以不能断言 length===1，
  // 要断言「列表里每个群的 userId 都等于他自己」。
  const lists = await Promise.all(alive.map(u => api('/v1/conversations', { token: u.token })));
  const leaked = [];
  lists.forEach((l, i) => {
    for (const c of l) {
      if (c.userId !== alive[i].user.id) leaked.push(`${alive[i].phone.slice(-4)} 看到了 ${c.userId} 的群`);
    }
  });
  ok(`列表里没有别人的群（每人 ${lists.map(l => l.length).join('/')} 个）`, leaked.length === 0, leaked.slice(0, 2).join(' | '));

  // A 不能读 B 的群
  const crossRead = await Promise.all(
    alive.slice(0, 5).map((u, i) => {
      const other = convs[(i + 1) % convs.length];
      return api('/v1/conversations/' + other.id, { token: u.token, expectFail: true });
    })
  );
  ok('读不到别人的群', crossRead.every(r => r.failed));

  // ---- 5. 同一会话并发（多标签页）----
  const u0 = alive[0];
  const c0 = convs[0];

  // 记下并发前的基线——这个群在第 3 步已经说过一轮了
  const beforeMsgs = await api(`/v1/conversations/${c0.id}/messages?limit=200`, { token: u0.token });
  const beforeUser = (Array.isArray(beforeMsgs) ? beforeMsgs : beforeMsgs.messages)
    .filter(m => m.role === 'user').length;

  const same = await Promise.all(
    Array.from({ length: 4 }, (_, i) => speak(u0.token, c0.id, `同一会话并发 ${i}`))
  );
  const busyCount = same.filter(r => r.busy).length;
  const okCount = same.filter(r => r.status === 200).length;
  ok(`同一会话并发被串行化（${okCount} 接受 / ${busyCount} 标记 busy）`,
     same.every(r => r.status === 200) && busyCount >= 2,
     same.map(r => `${r.status}${r.busy ? 'b' : ''}`).join(','));

  // 消息不重复：4 次发言 = 4 条用户消息
  const msgs = await api(`/v1/conversations/${c0.id}/messages?limit=200`, { token: u0.token });
  const list = Array.isArray(msgs) ? msgs : msgs.messages;
  const userMsgs = list.filter(m => m.role === 'user').length;
  const added = userMsgs - beforeUser;
  ok(`并发发言全部落库（新增 ${added} 条用户消息）`, added === 4, `基线 ${beforeUser}，现在 ${userMsgs}`);

  // ---- 6. 额度不丢更新 ----
  await Promise.all(alive.map((u, i) =>
    speak(u.token, convs[i].id, `第 2 轮`).catch(() => {})
  ));
  const mes = await Promise.all(alive.map(u => api('/v1/me', { token: u.token })));
  // 每个用户的 used 应该 = 他自己群里助手消息数（开场白不算）
  const mismatched = [];
  for (let i = 0; i < alive.length; i++) {
    const m = await api(`/v1/conversations/${convs[i].id}/messages?limit=200`, { token: alive[i].token });
    const l = Array.isArray(m) ? m : m.messages;
    const assistant = l.filter(x => x.role === 'assistant').length - 1; // 减开场白
    const delta = mes[i].user.used - baseline[i];
    if (delta !== assistant) {
      mismatched.push(`${alive[i].phone.slice(-4)}: Δused=${delta} assistant=${assistant} 基线=${baseline[i]}`);
    }
  }
  ok('额度与实际调用次数对得上', mismatched.length === 0, mismatched.slice(0, 3).join(' | '));

  // ---- 7. 清理 ----
  await Promise.all(alive.map((u, i) =>
    fetch(`${BASE}/v1/conversations/${convs[i].id}`, {
      method: 'DELETE', headers: { Authorization: 'Bearer ' + u.token },
    }).catch(() => {})
  ));

  const dur = ((Date.now() - t0) / 1000).toFixed(1);
  console.log(`\n${pass} 通过, ${fail} 失败 · 总耗时 ${dur}s\n`);
  if (fail) { problems.forEach(p => console.log('  - ' + p)); process.exit(1); }
})();
