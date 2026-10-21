#!/usr/bin/env node
/**
 * 不稳定网络的韧性测试。
 *
 *   node scripts/resilience.mjs
 *
 * 本地测试永远不断网，但用户在地铁上、电梯里就会遇到：
 *   - SSE 流读到一半连接断了，后端还在跑吗？会不会白扣额度？
 *   - 流式输出中途刷新页面，已经说出来的话还在不在？
 *   - 同一条消息连点两次发送，会不会重复扣额度、重复发言？
 *   - 断开之后重新打开，历史是否完整、能否接着聊？
 */

const BASE = process.env.E2E_BASE || 'http://localhost:8080';

const sleep = ms => new Promise(r => setTimeout(r, ms));
let pass = 0, fail = 0;
const failed = [];
const ok = (n, c, d) => {
  if (c) { pass++; console.log(`  \x1b[32m✓\x1b[0m ${n}`); }
  else { fail++; failed.push(n); console.log(`  \x1b[31m✗\x1b[0m ${n}${d ? '  → ' + d : ''}`); }
};

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

const msgsOf = async (token, convId) => {
  const d = await api(`/v1/conversations/${convId}/messages?limit=200`, { token });
  return Array.isArray(d) ? d : d.messages;
};

(async () => {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来'); process.exit(2); }

  const phone = '136' + String(Date.now()).slice(-8);
  let code = '123456';
  try {
    const r = await api('/v1/auth/send-code', { method: 'POST', body: { kind: 'phone', target: phone } });
    code = r.devCode || code;
  } catch { /* 用固定码 */ }
  const auth = await api('/v1/auth/login', { method: 'POST', body: { kind: 'phone', target: phone, code } });
  const token = auth.token;

  console.log('\n不稳定网络韧性\n');

  // ---- 1. 流读到一半主动断开 ----
  const conv1 = await api('/v1/conversations', {
    method: 'POST', token, body: { title: '中断测试', studio: 'companion' },
  });

  const before = (await api('/v1/me', { token })).user.used;
  const ac = new AbortController();
  let received = '';
  let deltas = 0;

  try {
    const res = await fetch(`${BASE}/v1/conversations/${conv1.id}/orchestrate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
      body: JSON.stringify({ content: '说点什么，我中途会断线' }),
      signal: ac.signal,
    });
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      const chunk = dec.decode(value, { stream: true });
      if (chunk.includes('"delta"')) { deltas++; received += chunk; }
      // 收到几段就断，模拟地铁进隧道
      if (deltas >= 3) { ac.abort(); break; }
    }
  } catch (e) {
    if (e.name !== 'AbortError') throw e;
  }

  ok(`成功中断（收到 ${deltas} 段后断开）`, deltas > 0);

  // 等一会儿，让后端感知到 ctx 取消
  await sleep(2500);

  const msgs1 = await msgsOf(token, conv1.id);
  const userMsgs1 = msgs1.filter(m => m.role === 'user');
  const assist1 = msgs1.filter(m => m.role === 'assistant');

  ok('用户消息已落库（断开也不丢）', userMsgs1.length === 1);
  ok(`中断前说出来的话已保留（${assist1.length - 1} 条发言 + 开场白）`, assist1.length >= 1);

  // 后端应该因为 ctx 取消而停止，不该继续把所有角色跑完
  const afterAbort = (await api('/v1/me', { token })).user.used;
  ok(`中断后没有继续扣额度（${before} → ${afterAbort}）`, afterAbort - before <= 2);

  // ---- 2. 中断之后接着用 ----
  const conv2 = await api('/v1/conversations', {
    method: 'POST', token, body: { title: '恢复测试', studio: 'companion' },
  });
  const resumed = await fetch(`${BASE}/v1/conversations/${conv2.id}/orchestrate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ content: '刚才断了一下，重新说' }),
  });
  await resumed.text();
  ok('断开后重新发消息正常', resumed.status === 200);

  const msgs2 = await msgsOf(token, conv2.id);
  ok('恢复后的对话完整落库', msgs2.filter(m => m.role === 'user').length === 1);

  // ---- 3. 重复提交（连点两次）----
  const conv3 = await api('/v1/conversations', {
    method: 'POST', token, body: { title: '重复提交', studio: 'companion' },
  });
  const usedBeforeDup = (await api('/v1/me', { token })).user.used;

  const dupBody = JSON.stringify({ content: '这条我不小心点了两次' });
  const fire = () => fetch(`${BASE}/v1/conversations/${conv3.id}/orchestrate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: dupBody,
  }).then(r => r.text());

  const [t1, t2] = await Promise.all([fire(), fire()]);
  const busyCount = [t1, t2].filter(t => t.includes('"type":"busy"')).length;

  ok(`重复提交被串行化（${busyCount} 个 busy）`, busyCount >= 1);

  const msgs3 = await msgsOf(token, conv3.id);
  const sameText = msgs3.filter(m => m.role === 'user' && m.content.includes('点了两次')).length;
  ok(`重复提交的消息都在（${sameText} 条，不丢也不合并）`, sameText === 2);

  const usedAfterDup = (await api('/v1/me', { token })).user.used;
  const dupCost = usedAfterDup - usedBeforeDup;
  ok(`额度按实际发言扣，不重复计（+${dupCost}）`, dupCost <= 4);

  // ---- 4. 完全不发请求时不会自己扣额度 ----
  const usedIdle1 = (await api('/v1/me', { token })).user.used;
  await sleep(1500);
  const usedIdle2 = (await api('/v1/me', { token })).user.used;
  ok('空闲时不产生任何消耗', usedIdle1 === usedIdle2);

  // 清理
  for (const c of [conv1, conv2, conv3]) {
    await fetch(`${BASE}/v1/conversations/${c.id}`, {
      method: 'DELETE', headers: { Authorization: 'Bearer ' + token },
    }).catch(() => {});
  }

  console.log(`\n${pass} 通过, ${fail} 失败\n`);
  if (fail) { failed.forEach(f => console.log('  - ' + f)); process.exit(1); }
})();
