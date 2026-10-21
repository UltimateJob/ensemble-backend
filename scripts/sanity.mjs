#!/usr/bin/env node
/**
 * 真实输出的「卫生检查」（需要真实 API Key）。
 *
 *   node scripts/sanity.mjs
 *
 * quality.mjs 看的是「有没有人说话、有没有接话」，这个看的是
 * 「说出来的东西干不干净」——那些只有真实模型才会犯、而且
 * mock 和单测都覆盖不到的问题：
 *
 *   · 替别人说话（"立华奏：…" 出现在 Saber 的发言里）
 *   · 出戏（"作为一个 AI" / "很高兴为您服务"）
 *   · 空发言、只剩标点
 *   · 复读上一条
 *   · 过长（群聊里没人发五百字）
 *   · 残留的 [PASS] 标记
 *
 * Round 65 就是靠眼睛看真实数据才发现「替别人说话」的。
 * 这个脚本把那种检查固定下来。
 */

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const ROUNDS = +(process.env.ROUNDS || 2);
const SCENES = ['bid', 'code', 'research', 'anime', 'companion'];
const PROMPTS = [
  '帮我看看这个事怎么推进',
  '这块我有点拿不准，你们怎么看',
  '已经做了一半，卡住了',
];

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

async function runRound(token, convId, text) {
  const res = await fetch(`${BASE}/v1/conversations/${convId}/orchestrate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ content: text }),
  });
  if (!res.ok) return [];
  const body = await res.text();
  const said = [];
  for (const line of body.split('\n')) {
    if (!line.startsWith('data:')) continue;
    let ev;
    try { ev = JSON.parse(line.slice(5).trim()); } catch { continue; }
    if (ev.type === 'delta' && ev.name) {
      const last = said[said.length - 1];
      if (last && last.name === ev.name) last.text += ev.content;
      else said.push({ name: ev.name, text: ev.content || '' });
    }
    // end 事件带的是后端清理过的正文（去掉说话人前缀、[PASS]）。
    // 前端会用它覆盖流式显示的内容，这里也要照做——
    // 否则检查的是原文，会误报。
    if (ev.type === 'end' && ev.name && typeof ev.content === 'string') {
      const last = said[said.length - 1];
      if (last && last.name === ev.name) last.text = ev.content;
    }
  }
  return said;
}

// ---- 检查规则 ----

const OFF_CHARACTER = [
  '作为一个AI', '作为一个 AI', '作为AI语言模型', '语言模型',
  '很高兴为您服务', '我是AI助手', '我是 AI 助手',
  '我无法协助', '我无法回答', '我不能提供',
];

function checkOne(name, text, prevTexts, allNames) {
  const problems = [];
  const t = (text || '').trim();

  if (!t) {
    problems.push('空发言');
    return problems;
  }

  // 替别人说话 / 自己的名字前缀
  for (const n of allNames) {
    if (t.startsWith(n + '：') || t.startsWith(n + ':')) {
      problems.push(`行首带说话人前缀「${n}：」`);
      break;
    }
  }
  // 正文中间出现「别人名字：」—— 说明在替人接话
  if (new RegExp(`[^\\s]{1,4}：.{2,}`).test(t) && /[\u4e00-\u9fa5]{2,4}：/.test(t.slice(1))) {
    const m = t.slice(1).match(/([\u4e00-\u9fa5]{2,4})：/);
    if (m && allNames.includes(m[1]) && m[1] !== name) {
      problems.push(`正文里替「${m[1]}」说话`);
    }
  }

  // 出戏
  for (const k of OFF_CHARACTER) {
    if (t.includes(k)) { problems.push(`出戏：出现「${k}」`); break; }
  }

  // 残留标记
  if (/\[PASS\]|【PASS】/.test(t)) problems.push('残留 [PASS] 标记');

  // 只剩标点
  if (!/[\u4e00-\u9fa5A-Za-z0-9]/.test(t)) problems.push('只有标点，没有内容');

  // 过长
  if ([...t].length > 220) problems.push(`过长（${[...t].length} 字）`);

  // 复读
  if (prevTexts.some(p => p && p.trim() === t)) problems.push('和之前某条完全一样');

  return problems;
}

(async () => {
  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来'); process.exit(2); }

  const me = await api('/v1/auth/login', {
    method: 'POST', body: { kind: 'phone', target: '13800000000' },
  });
  const token = me.token;
  if ((await api('/v1/me', { token })).demo) {
    console.error('\n当前是演示模式，检查不了真实输出。配 DEEPSEEK_API_KEY 再跑。\n');
    process.exit(2);
  }

  console.log(`\n输出卫生检查 · ${SCENES.length} 场景 × ${ROUNDS} 轮\n`);

  let totalSaid = 0, totalBad = 0;
  const report = [];

  for (const scene of SCENES) {
    const conv = await api('/v1/conversations', {
      method: 'POST', token,
      body: { title: `卫生检查 ${scene}`, studio: scene },
    });
    const convDetail = await api('/v1/conversations/' + conv.id, { token });
    const names = (convDetail.agents || []).map(a => a.name);

    // 场景角色可能来自场景定义（非随机场景 agents 为空时）
    if (!names.length) {
      const sc = (await api('/v1/scenarios', { token })).find(s => s.key === scene);
      (sc?.agents || []).forEach(a => names.push(a.name));
    }

    const seen = [];
    let saidCount = 0, badCount = 0;

    for (let r = 0; r < ROUNDS; r++) {
      const said = await runRound(token, conv.id, PROMPTS[r % PROMPTS.length]);
      for (const s of said) {
        saidCount++;
        const probs = checkOne(s.name, s.text, seen, names);
        seen.push(s.text);
        if (probs.length) {
          badCount++;
          report.push(`  [${scene}] ${s.name}：${probs.join('；')}\n      「${s.text.slice(0, 70)}」`);
        }
      }
    }

    totalSaid += saidCount;
    totalBad += badCount;
    const tag = badCount ? '\x1b[31m✗\x1b[0m' : '\x1b[32m✓\x1b[0m';
    console.log(`  ${tag} ${scene.padEnd(10)} ${saidCount} 条发言，${badCount} 条有问题`);

    await fetch(`${BASE}/v1/conversations/${conv.id}`, {
      method: 'DELETE', headers: { Authorization: 'Bearer ' + token },
    }).catch(() => {});
    await sleep(300);
  }

  console.log('');
  if (report.length) {
    console.log('问题明细：');
    report.forEach(r => console.log(r));
    console.log('');
  }
  console.log(`共 ${totalSaid} 条发言，${totalBad} 条有问题\n`);
  process.exit(totalBad > 0 ? 1 : 0);
})();
