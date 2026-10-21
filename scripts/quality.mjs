#!/usr/bin/env node
/**
 * 五个场景的真实对话质量检查。
 *
 * 不走 mock：真调 DeepSeek，看角色表现有没有因为这些年的编排改动而退化
 * （太容易沉默、全在复读、没人接话、专业内容消失）。
 *
 * 用法：
 *   DEEPSEEK_API_KEY=sk-... ./bin/ensemble &
 *   node scripts/quality.mjs
 */

const BASE = process.env.E2E_BASE || 'http://localhost:8080';
const KEY = process.env.DEEPSEEK_API_KEY || '';

const SCENES = [
  { key: 'bid', name: '竞标工作室', ask: '华东医院数据中心，必须 ISO 27001，近三年 3 个同类案例，要求国产化信创。15 天截止。' },
  { key: 'code', name: '代码工作室', ask: '做个功能：用户上传 Excel 后自动生成图表，并能导出 PNG。' },
  { key: 'research', name: '研报工作室', ask: '分析一下 2024 年国内储能行业的装机量和增速。' },
  { key: 'anime', name: '二次元茶话会', ask: '今天好累啊，不想动。' },
  { key: 'companion', name: '日常陪聊', ask: '最近有点迷茫，不知道要不要换工作。' },
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

/** 读 SSE，返回事件列表 */
function readSSE(text) {
  const out = [];
  for (const line of text.split('\n')) {
    const t = line.trim();
    if (!t.startsWith('data:')) continue;
    try { out.push(JSON.parse(t.slice(5).trim())); } catch { /* 忽略 */ }
  }
  return out;
}

async function runScene(token, scene) {
  const conv = await api('/v1/conversations', {
    method: 'POST', token,
    body: { title: `质量检查 · ${scene.name}`, studio: scene.key },
  });

  const started = Date.now();
  const res = await fetch(`${BASE}/v1/conversations/${conv.id}/orchestrate`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ content: scene.ask }),
  });
  const text = await res.text();
  const events = readSSE(text);

  // 按角色拼出完整发言
  const speeches = {};
  let passes = 0;
  let asks = 0;
  let current = null;
  for (const ev of events) {
    if (ev.type === 'start') { current = ev.name; speeches[current] = ''; }
    else if (ev.type === 'delta' && current) speeches[current] += ev.content;
    else if (ev.type === 'pass') passes++;
    else if (ev.type === 'ask') asks++;
  }
  const said = Object.entries(speeches).filter(([, v]) => v.trim().length > 0);

  // 清理
  await fetch(`${BASE}/v1/conversations/${conv.id}`, {
    method: 'DELETE', headers: { Authorization: 'Bearer ' + token },
  }).catch(() => {});

  return {
    scene: scene.name,
    ms: Date.now() - started,
    speakers: said.length,
    passes,
    asks,
    totalLen: said.reduce((a, [, v]) => a + v.length, 0),
    lines: said,
  };
}

/** 检查发言之间有没有互相接话：后一个是否提到前一个的独特词 */
function interlock(lines) {
  if (lines.length < 2) return 0;
  let hits = 0;
  for (let i = 1; i < lines.length; i++) {
    const prev = lines[i - 1][1];
    const cur = lines[i][1];
    // 取前一句里长度 >=2 的中文片段，看看后一句用没用上
    const grams = new Set();
    for (let j = 0; j < prev.length - 1; j++) {
      const g = prev.slice(j, j + 2);
      if (/^[\u4e00-\u9fa5]{2}$/.test(g)) grams.add(g);
    }
    let hit = false;
    for (const g of grams) {
      if (cur.includes(g)) { hit = true; break; }
    }
    if (hit) hits++;
  }
  return hits;
}

(async () => {
  if (!KEY && process.env.REQUIRE_LIVE === '1') {
    console.error('需要 DEEPSEEK_API_KEY');
    process.exit(2);
  }

  const health = await fetch(`${BASE}/healthz`).catch(() => null);
  if (!health || !health.ok) { console.error('后端没起来'); process.exit(2); }

  const { token } = await api('/v1/auth/login', {
    method: 'POST', body: { kind: 'phone', target: '13800000000' },
  });

  console.log('\n五个场景的真实对话质量\n');

  // 单次采样波动很大（同样的输入，两次可能 1 人 vs 4 人发言），
  // 所以每场跑两遍取平均，否则很容易误判。
  const ROUNDS = +(process.env.ROUNDS || 2);
  const results = [];
  for (const scene of SCENES) {
    process.stdout.write(`  ${scene.name} … `);
    const runs = [];
    for (let i = 0; i < ROUNDS; i++) {
      try {
        runs.push(await runScene(token, scene));
      } catch (e) {
        console.log('失败：' + e.message);
      }
      await sleep(300);
    }
    if (!runs.length) { results.push({ scene: scene.name, error: '全部失败' }); continue; }

    const merged = {
      scene: scene.name,
      speakers: runs.reduce((a, r) => a + r.speakers, 0) / runs.length,
      passes: runs.reduce((a, r) => a + r.passes, 0) / runs.length,
      asks: runs.reduce((a, r) => a + r.asks, 0),
      totalLen: runs.reduce((a, r) => a + r.totalLen, 0) / runs.length,
      lock: runs.reduce((a, r) => a + interlock(r.lines), 0) / runs.length,
      lines: runs[runs.length - 1].lines,
      ms: runs.reduce((a, r) => a + r.ms, 0) / runs.length,
    };
    results.push(merged);
    console.log(`${merged.speakers.toFixed(1)} 人发言 · 沉默 ${merged.passes.toFixed(1)} · ${Math.round(merged.totalLen / Math.max(1, merged.speakers))} 字/人 · 接话 ${merged.lock.toFixed(1)}`);
    console.log('');
    for (const [name, text] of merged.lines) {
      console.log(`    ${name}：${text.replace(/\s+/g, ' ').slice(0, 88)}`);
    }
    console.log('');
  }

  // 汇总
  const ok = results.filter(r => !r.error);
  const avgSpeakers = ok.reduce((a, r) => a + r.speakers, 0) / ok.length;
  const totalPasses = ok.reduce((a, r) => a + r.passes, 0);
  const avgLock = ok.reduce((a, r) => a + (r.lock || 0), 0) / ok.length;
  const anyAsk = ok.reduce((a, r) => a + r.asks, 0);

  console.log('─'.repeat(56));
  console.log(`平均每场 ${avgSpeakers.toFixed(1)} 人发言`);
  console.log(`沉默 ${totalPasses} 人次，主动提问 ${anyAsk} 次`);
  console.log(`平均接话 ${avgLock.toFixed(1)} 处（后一句引用了前一句的内容）`);
  console.log('─'.repeat(56));

  // 健康判断
  const problems = [];
  for (const r of ok) {
    if (r.speakers === 0) problems.push(`${r.scene}：全场沉默`);
    if (r.speakers > 0 && r.totalLen / r.speakers < 12) problems.push(`${r.scene}：发言过短（可能退化成敷衍）`);
  }
  // 阈值按「健康区间」而不是「越多越好」：
  // 有人问用户时，其他人等回答是正常的，不该算问题
  if (avgSpeakers < 1.5) problems.push('整体发言人数偏低，可能 PASS 判定太严');
  if (avgLock < 0.5) problems.push('角色之间几乎不接话，像是在各说各的');

  if (problems.length) {
    console.log('\n发现的问题：');
    problems.forEach(p => console.log('  ! ' + p));
    process.exit(1);
  }
  console.log('\n质量检查通过\n');
})();
