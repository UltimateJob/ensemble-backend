#!/usr/bin/env node
/**
 * 智团 · 团队 vs 单聊 基准测试
 *
 * 用同一套任务、同一个模型、同一个工作区约束，分别跑：
 *   solo  —— 一个通用全栈 Agent，自己从头做到尾（1v1 基线）
 *   team  —— 架构拆解 → 前后端并行 → 测试验收（团队流水线）
 *
 * 指标：总耗时、各阶段耗时、步数、token、工具调用、改动文件、验证结果。
 *
 * 用法：
 *   BENCH_BASE=http://127.0.0.1:8091 node scripts/bench/bench.mjs [caseId]
 *
 * 前置：
 *   - 基准后端跑在 :8091，接真实模型（见 scripts/bench/README.md）
 *   - 演示账号可登录（DEV_BYPASS=true）
 *
 * 产物：
 *   /tmp/ensemble-bench/<runId>/<mode>/       每次的工作区
 *   docs/bench/<runId>-report.json            原始数据
 *   docs/bench/<runId>-report.md              人读报告
 */
import { spawn } from 'node:child_process';
import {
  mkdirSync, writeFileSync, readdirSync, statSync, readFileSync, rmSync, existsSync,
} from 'node:fs';
import { join } from 'node:path';
import net from 'node:net';

const BASE = process.env.BENCH_BASE || 'http://127.0.0.1:8091';
const OUT_ROOT = process.env.BENCH_OUT || '/tmp/ensemble-bench';
const REPORT_DIR = process.env.BENCH_REPORT_DIR || 'docs/bench';
const PHONE = process.env.BENCH_PHONE || '13800000000';
const ONLY = process.argv[2] || '';
const RUN_TIMEOUT_MS = Number(process.env.BENCH_RUN_TIMEOUT_MS || 25 * 60 * 1000);

const sleep = ms => new Promise(r => setTimeout(r, ms));

/* ---------------- HTTP ---------------- */
async function call(method, path, body, token) {
  const headers = { 'Content-Type': 'application/json' };
  if (token) headers.Authorization = 'Bearer ' + token;
  const res = await fetch(BASE + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let json = {};
  try { json = await res.json(); } catch { /* 非 JSON */ }
  if (!res.ok || json.ok === false) {
    const err = new Error((json.error && json.error.message) || ('HTTP ' + res.status));
    err.status = res.status;
    err.code = json.error && json.error.code;
    throw err;
  }
  return json.data;
}

async function login() {
  const d = await call('POST', '/v1/auth/login', {
    kind: 'phone', target: PHONE, code: '123456',
  });
  return d.token;
}

/* ---------------- 工作区工具 ---------------- */
function walk(dir, base = '') {
  let out = [];
  let entries;
  try { entries = readdirSync(dir, { withFileTypes: true }); } catch { return out; }
  for (const e of entries) {
    if (e.name === '.git' || e.name === 'node_modules') continue;
    const rel = base ? base + '/' + e.name : e.name;
    const full = join(dir, e.name);
    if (e.isDirectory()) out = out.concat(walk(full, rel));
    else {
      let size = 0;
      try { size = statSync(full).size; } catch { /* ignore */ }
      out.push({ path: rel, size });
    }
  }
  return out;
}

function fileText(root, rel) {
  try { return readFileSync(join(root, rel), 'utf8'); } catch { return ''; }
}

async function killPort(port) {
  if (process.platform === 'win32') return;
  await new Promise(resolve => {
    const p = spawn('sh', ['-c', `lsof -ti tcp:${port} | xargs kill -9 2>/dev/null || true`]);
    p.on('close', resolve);
    p.on('error', resolve);
  });
  await sleep(300);
}

async function waitForPort(port, timeoutMs) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    if (await portInUse(port)) return true;
    await sleep(300);
  }
  return false;
}

function portInUse(port) {
  return new Promise(resolve => {
    let settled = false;
    const done = v => {
      if (settled) return;
      settled = true;
      sock.destroy();
      resolve(v);
    };
    const sock = net.connect({ host: '127.0.0.1', port }, () => done(true));
    sock.on('error', () => done(false));
    sock.setTimeout(1500, () => done(false));
  });
}

function runCmd(cmd, args, cwd, timeoutMs = 120000) {
  return new Promise(resolve => {
    const p = spawn(cmd, args, { cwd, env: { ...process.env, GOFLAGS: '-mod=mod' } });
    let out = '', err = '';
    const timer = setTimeout(() => { try { p.kill('SIGKILL'); } catch { /* ignore */ } }, timeoutMs);
    p.stdout.on('data', d => { out += d; });
    p.stderr.on('data', d => { err += d; });
    p.on('close', code => {
      clearTimeout(timer);
      resolve({ code, out: out.slice(-4000), err: err.slice(-4000) });
    });
    p.on('error', e => {
      clearTimeout(timer);
      resolve({ code: -1, out: '', err: e.message });
    });
  });
}

/* ---------------- 任务定义 ---------------- */
const CASES = [
  {
    id: 'todo-fullstack',
    title: '待办 Web 系统（Go API + 静态前端）',
    kind: 'complex',
    task: [
      '做一个待办事项 Web 系统：',
      '- 后端用 Go 标准库提供 REST API：列表、新增、切换完成、删除，内存存储；必须放在 backend/ 目录。',
      '- 前端是一个静态页面，能添加、勾选完成、删除待办；必须放在 web/ 目录，由后端同源托管。',
      '- 接口统一返回 JSON；新增返回 201，删除返回 204，找不到返回 404。',
      '- 接口契约固定：新增 `POST /api/todos`，请求体 {"text":"买牛奶"}，返回 201；',
      '  切换完成 `PATCH /api/todos/{id}`，请求体 {"done":true}；删除 `DELETE /api/todos/{id}`，返回 204。',
      '- 后端要有单元测试；根目录写 README.md（启动、测试、接口说明）。',
      '- 要求 `cd backend && go test ./...` 通过，`go run . -addr :18080 -web ../web` 能启动。',
      '- 不需要自己启动服务做 curl 冒烟：评委会用独立进程做四接口冒烟。',
    ].join('\n'),
    constraints: [
      '后端只能改 backend/、cmd/、internal/、*.go、go.mod、go.sum。',
      '前端只能改 web/、public/、*.html、*.css、*.js。',
    ],
    verify: {
      cmd: ['go', ['test', './...'], 'backend'],
      checks: [
        { name: '后端测试通过', fn: r => r.verify.code === 0 },
        { name: '前端页面存在', fn: r => r.files.some(f => f.path === 'web/index.html') },
        { name: '前端脚本存在', fn: r => r.files.some(f => /^web\/.*\.js$/.test(f.path)) },
        { name: '后端入口存在', fn: r => r.files.some(f => /^backend\/.*\.go$/.test(f.path)) },
        { name: 'README 存在', fn: r => r.files.some(f => /^README(\.md)?$/i.test(f.path)) },
        { name: 'API 覆盖四种操作', fn: r => apiCoverage(r) >= 4 },
        { name: '前后端接口一致', fn: r => contractConsistent(r) },
        { name: '新增字段遵循契约(text)', fn: r => contractField(r, 'text') },
      ],
    },
    smoke: {
      cmd: ['go', ['run', '.', '-addr', '127.0.0.1:18080', '-web', '../web'], 'backend'],
      requests: [
        { method: 'GET', path: '/api/todos', expect: 200 },
        { method: 'POST', path: '/api/todos', body: { text: '基准测试' }, expect: [200, 201] },
        // {id} 用上一步 POST 返回的真实 id，避免依赖「第一个 id 一定是 1」。
        { method: 'PATCH', path: '/api/todos/{id}', body: { done: true }, expect: [200, 204] },
        { method: 'DELETE', path: '/api/todos/{id}', expect: [200, 204] },
        { method: 'GET', path: '/', expect: 200 },
      ],
    },
  },
  {
    id: 'seeded-bugfix',
    title: '已有项目跨端修 bug（后端校验 + 前端展示）',
    kind: 'complex',
    seed: seedBugfixRepo,
    task: [
      '这个仓库有一个待办系统，现在有两个 bug：',
      '1) 后端：新增待办时 `text` 为空字符串也应该返回 400，但现在会成功；另外 `text` 超过 200 个字符（按 Unicode 字符数/rune 计算，不要用字节数；不要额外 trim 空格）也必须返回 400。',
      '2) 前端：接口返回错误时页面没有任何提示，用户只看到没反应。需要在页面上显示错误信息（取 error.message）。',
      '要求：修好这两个问题，保持现有测试通过；补一条后端测试覆盖空文本和超长文本；不要改接口路径和字段名。',
    ].join('\n'),
    constraints: [
      '后端只能改 backend/、*.go、go.mod。',
      '前端只能改 web/、*.html、*.css、*.js。',
    ],
    verify: {
      cmd: ['go', ['test', './...'], 'backend'],
      checks: [
        { name: '后端测试通过', fn: r => r.verify.code === 0 },
        { name: '新增了校验测试', fn: r => /empty|blank|too.?long|200|400|空|超长|长度/i.test(fileText(r.workspace, 'backend/handler_test.go')) },
        { name: '前端有错误展示', fn: r => /error/i.test(fileText(r.workspace, 'web/app.js')) && /error|错误/i.test(fileText(r.workspace, 'web/index.html') + fileText(r.workspace, 'web/app.js')) },
      ],
    },
    smoke: {
      cmd: ['go', ['run', '.', '-addr', '127.0.0.1:18081', '-web', '../web'], 'backend'],
      requests: [
        { method: 'POST', path: '/api/todos', body: { text: '' }, expect: 400 },
        { method: 'POST', path: '/api/todos', body: { text: 'x'.repeat(201) }, expect: 400 },
        { method: 'POST', path: '/api/todos', body: { text: '正常' }, expect: [200, 201] },
        { method: 'GET', path: '/', expect: 200 },
      ],
    },
  },
  {
    id: 'simple-kata',
    title: '简单任务：LRU 缓存 + 测试',
    kind: 'simple',
    task: [
      '用 Go 写一个线程安全的 LRU 缓存（容量固定，Get/Put，淘汰最久未使用），放在 lru/ 目录。',
      '要求：Get 未命中返回 (零值,false)；Put 已存在时更新并移到最近使用；容量 <= 0 时视为 1。',
      '写表驱动测试，覆盖命中、未命中、淘汰、更新顺序、并发读写（-race 通过）。',
      '`go test ./...` 必须通过。',
    ].join('\n'),
    constraints: ['只能改 lru/、go.mod、go.sum。'],
    verify: {
      cmd: ['go', ['test', '-race', './...'], 'lru'],
      checks: [
        { name: '测试通过（含 -race）', fn: r => r.verify.code === 0 },
        { name: '实现文件存在', fn: r => r.files.some(f => /^lru\/.*\.go$/.test(f.path)) },
        { name: '有测试文件', fn: r => r.files.some(f => /^lru\/.*_test\.go$/.test(f.path)) },
      ],
    },
  },
];

/* ---------------- 仓库种子 ---------------- */
function seedBugfixRepo(root) {
  mkdirSync(join(root, 'backend'), { recursive: true });
  mkdirSync(join(root, 'web'), { recursive: true });
  writeFileSync(join(root, 'backend', 'go.mod'), 'module todo\n\ngo 1.21\n');
  writeFileSync(join(root, 'backend', 'main.go'), `package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	web := flag.String("web", "../web", "web dir")
	flag.Parse()
	mux := http.NewServeMux()
	register(mux)
	mux.Handle("/", http.FileServer(http.Dir(*web)))
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
`);
  writeFileSync(join(root, 'backend', 'handler.go'), `package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type Todo struct {
	ID   int    \`json:"id"\`
	Text string \`json:"text"\`
	Done bool   \`json:"done"\`
}

type store struct {
	mu    sync.Mutex
	next  int
	items []Todo
}

var st = &store{next: 1}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"message": msg}})
}

func register(mux *http.ServeMux) {
	mux.HandleFunc("/api/todos", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			st.mu.Lock()
			out := append([]Todo(nil), st.items...)
			st.mu.Unlock()
			writeJSON(w, 200, map[string]any{"todos": out})
		case http.MethodPost:
			var in struct{ Text string \`json:"text"\` }
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeErr(w, 400, "请求格式不正确")
				return
			}
			if strings.TrimSpace(in.Text) == "" {
				writeErr(w, 400, "text 不能为空")
				return
			}
			st.mu.Lock()
			t := Todo{ID: st.next, Text: in.Text}
			st.next++
			st.items = append(st.items, t)
			st.mu.Unlock()
			writeJSON(w, 201, t)
		default:
			writeErr(w, 405, "method not allowed")
		}
	})
	mux.HandleFunc("/api/todos/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, 404, "not found")
	})
}
`);
  writeFileSync(join(root, 'backend', 'handler_test.go'), `package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateAndList(t *testing.T) {
	st = &store{next: 1}
	mux := http.NewServeMux()
	register(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/todos", strings.NewReader(\`{"text":"买牛奶"}\`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/todos", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "买牛奶") {
		t.Fatalf("list body=%s", rec.Body.String())
	}
}
`);
  writeFileSync(join(root, 'web', 'index.html'), `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>待办</title></head>
<body>
<h1>待办</h1>
<form id="form"><input id="text" required><button>添加</button></form>
<ul id="list"></ul>
<script src="app.js"></script>
</body>
</html>
`);
  writeFileSync(join(root, 'web', 'app.js'), `const list = document.getElementById('list');
async function load() {
  const res = await fetch('/api/todos');
  const data = await res.json();
  list.innerHTML = '';
  for (const t of data.todos) {
    const li = document.createElement('li');
    li.textContent = t.text;
    list.appendChild(li);
  }
}
document.getElementById('form').addEventListener('submit', async e => {
  e.preventDefault();
  const text = document.getElementById('text').value;
  await fetch('/api/todos', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  });
  load();
});
load();
`);
}

/* ---------------- 静态检查 ---------------- */
function apiCoverage(r) {
  const texts = r.files
    .filter(f => /^backend\/.*\.go$/.test(f.path))
    .map(f => fileText(r.workspace, f.path))
    .join('\n');
  let n = 0;
  if (/MethodGet|"GET"/.test(texts)) n++;
  if (/MethodPost|"POST"/.test(texts)) n++;
  if (/MethodPatch|"PATCH"/.test(texts)) n++;
  if (/MethodDelete|"DELETE"/.test(texts)) n++;
  return n;
}

function contractConsistent(r) {
  const fe = r.files.filter(f => /^web\/.*\.(js|html)$/.test(f.path))
    .map(f => fileText(r.workspace, f.path)).join('\n');
  const be = r.files.filter(f => /^backend\/.*\.go$/.test(f.path))
    .map(f => fileText(r.workspace, f.path)).join('\n');
  if (!fe || !be) return false;
  const fePaths = new Set([...fe.matchAll(/['"`](\/api\/[^'"`\s]+)['"`]/g)].map(m => m[1]));
  const bePaths = new Set([...be.matchAll(/["'`](\/api\/[^"'`\s]*)["'`]/g)].map(m => m[1]));
  // 前端至少引用一个后端真实存在的路径前缀
  for (const p of fePaths) {
    for (const q of bePaths) {
      if (p === q || q.startsWith(p.replace(/\/$/, '')) || p.startsWith(q.replace(/\/$/, ''))) return true;
    }
  }
  return false;
}

function contractField(r, field) {
  const fe = r.files.filter(f => /^web\/.*\.(js|html)$/.test(f.path))
    .map(f => fileText(r.workspace, f.path)).join('\n');
  const be = r.files.filter(f => /^backend\/.*\.go$/.test(f.path))
    .map(f => fileText(r.workspace, f.path)).join('\n');
  // 后端 JSON tag 和前端的请求体字段都要用契约里指定的名字。
  const feUses = new RegExp('\\b' + field + '\\s*:|\\{\\s*' + field + '\\s*\\}').test(fe);
  return new RegExp('json:"' + field + '"').test(be) && feUses;
}

/* ---------------- 运行模式 ---------------- */
function agentList(mode) {
  if (mode === 'solo') {
    return {
      members: ['solo'],
      agents: [{
        key: 'solo',
        name: '单聊工程师',
        title: '全栈',
        emoji: '单',
        color: '#0F766E',
        prompt: '你是一名资深全栈工程师。你独自负责这个任务：从设计到实现、测试、文档都由你完成。务实、直接，先读现有代码再动手。',
      }],
      permissions: { solo: 'full' },
      defaultPermission: 'full',
      studio: 'custom',
    };
  }
  return {
    members: ['arch', 'backend', 'frontend', 'qa'],
    agents: [],
    permissions: { arch: 'readonly', backend: 'full', frontend: 'full', qa: 'audit' },
    defaultPermission: 'full',
    studio: 'code',
  };
}

async function waitRun(token, runId) {
  const started = Date.now();
  while (Date.now() - started < RUN_TIMEOUT_MS) {
    const r = await call('GET', '/v1/runs/' + runId, undefined, token);
    if (!['running', 'waiting_approval'].includes(r.status)) return r;
    await sleep(1500);
  }
  throw new Error('run 超时：' + runId);
}

async function execute(token, convId, { content, keys, task, silent }) {
  const body = { content, agentKeys: keys, silent: !!silent };
  if (task) body.task = task;
  const d = await call('POST', `/v1/conversations/${convId}/execute`, body, token);
  const runs = d.runs || [];
  const errors = runs.filter(r => r.error);
  if (errors.length) throw new Error(errors.map(r => r.agentName + ': ' + r.error).join('; '));
  return runs;
}

async function runSolo(token, conv, c, work) {
  const t0 = Date.now();
  const runs = await execute(token, conv.id, {
    content: c.task,
    keys: ['solo'],
  });
  const done = await waitRun(token, runs[0].runId);
  return {
    mode: 'solo',
    wallMs: Date.now() - t0,
    phases: [{ name: '实现', wallMs: Date.now() - t0 }],
    runs: [summarizeRun(done)],
    status: done.status,
    error: done.error || '',
  };
}

async function runTeam(token, conv, c, work) {
  const t0 = Date.now();
  // 团队编排在后端：规划 → 分工 → 并行 → 验证 → 返工 → 评分。
  // 基准测试不再自己拼提示词，和桌面端走完全相同的接口。
  const created = await call('POST', `/v1/conversations/${conv.id}/team`, { task: c.task }, token);
  const teamID = created.team.id;
  let view = created.team;
  const phases = [];
  let lastPhase = '';
  const phaseStart = new Map();
  while (true) {
    if (!['planning', 'building', 'verifying', 'reviewing', 'repairing'].includes(view.status)) break;
    if (view.phase && view.phase !== lastPhase) {
      if (lastPhase) phases.push({ name: phaseLabel(lastPhase), wallMs: Date.now() - (phaseStart.get(lastPhase) || t0) });
      lastPhase = view.phase;
      phaseStart.set(view.phase, Date.now());
    }
    if (Date.now() - t0 > RUN_TIMEOUT_MS) {
      await call('POST', `/v1/teams/${teamID}/cancel`, {}, token).catch(() => {});
      break;
    }
    await sleep(1500);
    view = await call('GET', '/v1/teams/' + teamID, undefined, token);
  }
  if (lastPhase) phases.push({ name: phaseLabel(lastPhase), wallMs: Date.now() - (phaseStart.get(lastPhase) || t0) });

  // 拉取团队产生的全部 Run 明细（每个阶段的行）
  const runIds = [...new Set((view.phases || []).flatMap(p => p.runIds || []))];
  const runs = [];
  for (const rid of runIds) {
    try { runs.push(await call('GET', '/v1/runs/' + rid, undefined, token)); } catch { /* run 可能已清理 */ }
  }
  return {
    mode: 'team',
    teamId: teamID,
    wallMs: Date.now() - t0,
    phases,
    runs: runs.map(summarizeRun),
    status: view.status,
    score: view.score,
    repairs: view.repairs,
    checks: (view.checks || []).map(c => ({ name: c.name, passed: c.passed, skipped: c.skipped, exitCode: c.exitCode, output: (c.output || '').slice(0, 600) })),
    review: view.review || null,
    assignments: view.assignments || [],
    issues: view.issues || [],
    plan: view.plan || null,
    error: view.error || '',
  };
}

function phaseLabel(p) {
  return ({ planning: '规划', building: '并行开发', verifying: '验证', reviewing: '验收', repairing: '返工' })[p] || p;
}

function summarizeRun(r) {
  return {
    id: r.id,
    agentKey: r.operatorKey || r.operatorName,
    agentName: r.operatorName || r.operatorKey,
    status: r.status,
    steps: r.step || 0,
    tokensIn: r.tokensIn || 0,
    tokensOut: r.tokensOut || 0,
    toolCalls: (r.toolCalls || []).length,
    changedFiles: (r.diffs || []).length,
    summary: (r.summary || '').slice(0, 1200),
    error: r.error || '',
    elapsedMs: r.startedAt && r.endedAt
      ? new Date(r.endedAt).getTime() - new Date(r.startedAt).getTime()
      : 0,
  };
}

/* ---------------- 验证 ---------------- */
async function verifyCase(c, work) {
  const result = { workspace: work, files: walk(work), verify: { code: null, out: '', err: '' }, smoke: null };
  if (c.verify) {
    const [cmd, args, sub] = c.verify.cmd;
    const cwd = sub ? join(work, sub) : work;
    result.verify = await runCmd(cmd, args, cwd);
  }
  if (c.smoke) {
    const [cmd, args, sub] = c.smoke.cmd;
    const cwd = sub ? join(work, sub) : work;
    // 端口从 -addr 参数里取。之前用 args.includes('18081') 判断，
    // 但写的是 '127.0.0.1:18081'，永远匹配不上——两个用例都会去等 18080，
    // 冒烟请求打到残留进程/空端口，报告里全是 status 0。
    const addrIdx = args.findIndex(a => a === '-addr' || a === '--addr');
    const addr = addrIdx >= 0 ? String(args[addrIdx + 1] || '') : '';
    const port = Number(addr.split(':').pop()) || 18080;
    // 跳过冒烟的三类原因：没产出目录、端口被残留服务占用、启动失败。
    // 都要如实写进报告，不能把「没测」算成「通过」。
    if (!existsSync(cwd)) {
      result.smoke = { requests: [], log: `目录 ${cwd} 不存在，冒烟测试跳过`, skipped: true };
    } else if (await portInUse(port)) {
      // 上一轮 agent 起的常驻服务可能还没退干净。先清一次端口再试。
      await killPort(port);
      if (await portInUse(port)) {
        result.smoke = { requests: [], log: `端口 ${port} 已被占用，冒烟测试跳过`, skipped: true };
      }
    }
    if (!result.smoke) {
      // 冒烟前再确认一次端口干净（waitForPort 只判断「有人监听」，
      // 旧进程残留会让它误以为新服务已就绪）。
      await killPort(port);
      const proc = spawn(cmd, args, { cwd, env: { ...process.env, GOFLAGS: '-mod=mod' }, detached: true });
      let log = '';
      proc.on('error', e => { log += 'spawn error: ' + e.message; });
      proc.stdout.on('data', d => { log += d; });
      proc.stderr.on('data', d => { log += d; });
      // go run 冷启动要编译，2.5 秒不够。轮询等端口，最多 30 秒。
      const ready = await waitForPort(port, 30000);
      const smoke = { requests: [], log: ready ? '' : '服务在 30 秒内没有监听端口' };
      const vars = {};
      for (const r of c.smoke.requests) {
        const path = r.path.replace('{id}', vars.id ?? '1');
        const url = 'http://127.0.0.1:' + port + path;
        try {
          const res = await fetch(url, {
            method: r.method,
            headers: { 'Content-Type': 'application/json' },
            body: r.body ? JSON.stringify(r.body) : undefined,
            signal: AbortSignal.timeout(5000),
          });
          const text = await res.text();
          try {
            const parsed = JSON.parse(text);
            if (parsed && (typeof parsed.id === 'number' || typeof parsed.id === 'string')) vars.id = parsed.id;
          } catch { /* 非 JSON 响应不影响状态码检查 */ }
          const expect = Array.isArray(r.expect) ? r.expect : [r.expect];
          smoke.requests.push({ ...r, path, status: res.status, ok: expect.includes(res.status), body: text.slice(0, 300) });
        } catch (e) {
          smoke.requests.push({ ...r, path, status: 0, ok: false, body: e.message });
        }
      }
      // go run 会再 fork 一个真正的服务进程：只杀父进程会留下孤儿占着端口，
      // 下一次基准就会连到旧代码上。杀整个进程组，再按端口兜底清理。
      try { process.kill(-proc.pid, 'SIGKILL'); } catch { try { proc.kill('SIGKILL'); } catch { /* ignore */ } }
      await killPort(port);
      smoke.log = (ready ? '' : smoke.log + '\n') + log.slice(-1500);
      result.smoke = smoke;
    }
  }
  const checks = [];
  if (c.verify && c.verify.checks) {
    for (const ch of c.verify.checks) {
      let ok = false;
      try { ok = !!ch.fn(result); } catch { ok = false; }
      checks.push({ name: ch.name, ok });
    }
  }
  if (result.smoke) {
    if (result.smoke.skipped) {
      checks.push({ name: '冒烟测试已跳过：' + result.smoke.log, ok: false, skipped: true });
    } else {
      for (const r of result.smoke.requests) {
        checks.push({ name: `冒烟 ${r.method} ${r.path} → ${r.status}`, ok: r.ok });
      }
    }
  }
  result.checks = checks;
  const passed = checks.filter(x => x.ok).length;
  result.score = checks.length ? Math.round((passed / checks.length) * 100) : 0;
  return result;
}

/* ---------------- 主流程 ---------------- */
async function runOne(token, c, mode, runRoot) {
  const work = join(runRoot, mode);
  mkdirSync(work, { recursive: true });
  if (c.seed) c.seed(work);

  const spec = agentList(mode);
  const conv = await call('POST', '/v1/conversations', {
    title: `基准 ${c.id} · ${mode}`,
    studio: spec.studio,
    members: spec.members,
    agents: spec.agents.length ? spec.agents : undefined,
    workspace: { kind: 'local', root: work, label: c.id + '-' + mode },
    defaultPermission: spec.defaultPermission,
    agentPermissions: spec.permissions,
  }, token);

  let outcome;
  try {
    outcome = mode === 'solo'
      ? await runSolo(token, conv, c, work)
      : await runTeam(token, conv, c, work);
  } catch (e) {
    outcome = { mode, wallMs: 0, phases: [], runs: [], status: 'error', error: e.message };
  }
  await sleep(800); // 等落盘/最后一个 diff 完成
  const verification = await verifyCase(c, work);
  return {
    case: c.id,
    title: c.title,
    kind: c.kind,
    mode,
    convId: conv.id,
    outcome,
    verification,
  };
}

async function main() {
  const token = await login();
  const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
  const runRoot = join(OUT_ROOT, stamp);
  mkdirSync(runRoot, { recursive: true });
  mkdirSync(REPORT_DIR, { recursive: true });

  const cases = ONLY ? CASES.filter(c => c.id === ONLY) : CASES;
  const results = [];

  for (const c of cases) {
    // 简单任务只跑 solo：这是产品设计的一部分——一个 agent 能干完就不开团队。
    const modes = c.kind === 'simple' ? ['solo'] : ['solo', 'team'];
    for (const mode of modes) {
      console.log(`\n=== ${c.id} · ${mode} ===`);
      const r = await runOne(token, c, mode, join(runRoot, c.id));
      results.push(r);
      console.log(`  用时 ${Math.round(r.outcome.wallMs / 1000)}s · 状态 ${r.outcome.status} · 检查 ${r.verification.checks.filter(x => x.ok).length}/${r.verification.checks.length}`);
      for (const run of r.outcome.runs) {
        console.log(`  - ${run.agentName}: ${run.status}, ${run.steps} 步, ${run.tokensIn + run.tokensOut} tokens, ${run.changedFiles} 文件`);
      }
      for (const ch of r.verification.checks) {
        console.log(`    ${ch.ok ? '✓' : '✗'} ${ch.name}`);
      }
    }
  }

  const report = { stamp, base: BASE, results };
  writeFileSync(join(REPORT_DIR, stamp + '-report.json'), JSON.stringify(report, null, 2));
  writeFileSync(join(REPORT_DIR, stamp + '-report.md'), renderMarkdown(report));
  console.log(`\n报告：${join(REPORT_DIR, stamp + '-report.md')}`);
  console.log(`工作区：${runRoot}`);
}

function fmtMs(ms) {
  if (!ms) return '—';
  const s = Math.round(ms / 1000);
  return s < 60 ? s + 's' : Math.floor(s / 60) + 'm' + (s % 60) + 's';
}

function renderMarkdown(report) {
  const lines = [`# 基准测试报告 ${report.stamp}`, ''];
  const cases = [...new Set(report.results.map(r => r.case))];
  for (const id of cases) {
    const rows = report.results.filter(r => r.case === id);
    const solo = rows.find(r => r.mode === 'solo');
    const team = rows.find(r => r.mode === 'team');
    lines.push(`## ${rows[0].title}（${rows[0].kind}）`, '');
    if (!team) {
      lines.push('| 指标 | 单聊 |');
      lines.push('|---|---|');
      lines.push(`| 总耗时 | ${fmtMs(solo.outcome.wallMs)} |`);
      lines.push(`| 状态 | ${solo.outcome.status} |`);
      lines.push(`| 检查得分 | ${solo.verification.score} |`);
      lines.push('');
      lines.push('简单任务只跑单聊：一个 agent 能独立完成时，开团队是浪费。');
      lines.push('');
      lines.push('检查项：' + solo.verification.checks.map(c => (c.ok ? '✓' : '✗') + c.name).join('，'));
      lines.push('');
      for (const run of solo.outcome.runs) {
        lines.push(`- solo/${run.agentName}：${run.status}，${run.steps} 步，${fmtMs(run.elapsedMs)}，${run.tokensIn}+${run.tokensOut} tokens，${run.toolCalls} 次工具调用`);
      }
      lines.push('');
      continue;
    }
    lines.push('| 指标 | 单聊 | 团队 |');
    lines.push('|---|---|---|');
    lines.push(`| 总耗时 | ${fmtMs(solo.outcome.wallMs)} | ${fmtMs(team.outcome.wallMs)} |`);
    lines.push(`| 状态 | ${solo.outcome.status} | ${team.outcome.status} |`);
    lines.push(`| 检查得分 | ${solo.verification.score} | ${team.verification.score} |`);
    lines.push(`| 团队自评分 | — | ${team.outcome.score ?? '—'} |`);
    lines.push(`| 返工轮数 | — | ${team.outcome.repairs ?? 0} |`);
    const tok = r => r.outcome.runs.reduce((a, x) => a + x.tokensIn + x.tokensOut, 0);
    lines.push(`| tokens | ${tok(solo)} | ${tok(team)} |`);
    const steps = r => r.outcome.runs.reduce((a, x) => a + x.steps, 0);
    lines.push(`| 步数 | ${steps(solo)} | ${steps(team)} |`);
    lines.push(`| 改动文件 | ${solo.outcome.runs.reduce((a, x) => a + x.changedFiles, 0)} | ${team.outcome.runs.reduce((a, x) => a + x.changedFiles, 0)} |`);
    lines.push('');
    lines.push('检查项：');
    for (const r of rows) {
      lines.push(`- **${r.mode}**：${r.verification.checks.map(c => (c.ok ? '✓' : '✗') + c.name).join('，')}`);
    }
    lines.push('');
    if (team.outcome.checks && team.outcome.checks.length) {
      lines.push('团队服务端验证：' + team.outcome.checks.map(c => `${c.passed ? '✓' : '✗'}${c.name}`).join('，'));
    }
    if (team.outcome.review) {
      lines.push(`团队验收：${team.outcome.review.verdict}（${team.outcome.review.agentKey}）`);
    }
    if (team.outcome.assignments && team.outcome.assignments.length) {
      lines.push('团队分工：' + team.outcome.assignments.map(a => `${a.agentName || a.agentKey} → ${(a.scope || []).join('/') || '全工作区'}`).join('；'));
    }
    lines.push('');
    lines.push('阶段耗时（团队）：' + team.outcome.phases.map(p => `${p.name} ${fmtMs(p.wallMs)}`).join(' · '));
    lines.push('');
    lines.push('运行明细：');
    for (const r of rows) {
      for (const run of r.outcome.runs) {
        lines.push(`- ${r.mode}/${run.agentName}：${run.status}，${run.steps} 步，${fmtMs(run.elapsedMs)}，${run.tokensIn}+${run.tokensOut} tokens，${run.toolCalls} 次工具调用`);
      }
    }
    lines.push('');
  }
  return lines.join('\n');
}

main().catch(e => {
  console.error('基准中断：', e.message);
  process.exit(1);
});
