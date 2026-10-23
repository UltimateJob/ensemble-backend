#!/usr/bin/env node
/**
 * 基准测试用：OpenAI 兼容 → Anthropic Messages 协议的小代理。
 *
 * 为什么需要它：执行器的网关只讲 OpenAI 的 /chat/completions（含流式
 * tool_calls），而本机可用的模型通道是 Anthropic 协议。这个代理只做协议
 * 转换，不碰业务；仅用于本地基准，不进入产品运行时。
 *
 *   ANTHROPIC_BASE_URL=... ANTHROPIC_AUTH_TOKEN=... \
 *   node scripts/bench/anthropic-proxy.mjs
 *
 * 默认监听 127.0.0.1:8092，OpenAI 基地址填 http://127.0.0.1:8092/v1。
 */
import http from 'node:http';
import https from 'node:https';

const LISTEN = process.env.PROXY_ADDR || '127.0.0.1:8092';
const UPSTREAM = (process.env.ANTHROPIC_BASE_URL || 'https://api.anthropic.com').replace(/\/+$/, '');
const KEY = (process.env.ANTHROPIC_AUTH_TOKEN || process.env.ANTHROPIC_API_KEY || '').trim();
const DEFAULT_MODEL = process.env.ANTHROPIC_MODEL || 'deepseek-v4.1-flash';
// 默认不干预上游 thinking：实测对 tierflow_pro 传 disabled 反而让思考
// 从 14k 字符涨到 22k，6000 token 全被思考吃掉。需要时用 PROXY_THINKING=disabled 显式开。
const THINKING = (process.env.PROXY_THINKING || 'default').toLowerCase();
// 上游可能是 http（本地网关）或 https（官方/中转）；node:http 不支持 https。
const transport = UPSTREAM.startsWith('https:') ? https : http;

if (!KEY) {
  console.error('缺少 ANTHROPIC_AUTH_TOKEN / ANTHROPIC_API_KEY');
  process.exit(1);
}

function jsonResponse(res, status, obj) {
  const body = JSON.stringify(obj);
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(body),
  });
  res.end(body);
}

function openAIError(message, type = 'upstream_error') {
  return { error: { message, type, code: type } };
}

/* ---------------- OpenAI 请求 → Anthropic 请求 ---------------- */
function toAnthropic(req) {
  const messages = [];
  let system = '';

  const push = (role, content) => {
    if (!content.length) return;
    const last = messages[messages.length - 1];
    if (last && last.role === role) {
      last.content.push(...content);
    } else {
      messages.push({ role, content });
    }
  };

  for (const m of req.messages || []) {
    if (m.role === 'system') {
      system += (system ? '\n\n' : '') + String(m.content || '');
      continue;
    }
    if (m.role === 'user') {
      const content = typeof m.content === 'string'
        ? [{ type: 'text', text: m.content }]
        : (Array.isArray(m.content) ? m.content : []);
      push('user', content);
      continue;
    }
    if (m.role === 'assistant') {
      const blocks = [];
      if (m.content) blocks.push({ type: 'text', text: String(m.content) });
      for (const tc of m.tool_calls || []) {
        let input = {};
        try { input = JSON.parse(tc.function?.arguments || '{}'); } catch { input = {}; }
        blocks.push({
          type: 'tool_use',
          id: tc.id || `call_${blocks.length}`,
          name: tc.function?.name || 'tool',
          input,
        });
      }
      push('assistant', blocks);
      continue;
    }
    if (m.role === 'tool') {
      push('user', [{
        type: 'tool_result',
        tool_use_id: m.tool_call_id || '',
        content: String(m.content || ''),
      }]);
    }
  }

  const tools = (req.tools || []).map(t => ({
    name: t.function?.name || t.name,
    description: t.function?.description || '',
    input_schema: t.function?.parameters || { type: 'object', properties: {} },
  }));

  const out = {
    // 原样透传模型名；之前任何非 tierflow_pro 都会被替换成 DEFAULT_MODEL，
    // 导致 benchmark 选了 tierflow 也仍然跑 tierflow_pro。
    model: req.model || DEFAULT_MODEL,
    max_tokens: req.max_tokens || 8192,
    messages,
    stream: true,
  };
  if (system) out.system = system;
  if (THINKING === 'disabled') out.thinking = { type: 'disabled' };
  if (tools.length) {
    out.tools = tools;
    // OpenAI: auto | none | required | {function:{name}}
    // Anthropic: auto | none | any | {type:"tool",name}
    const tc = req.tool_choice;
    if (tc === 'required') out.tool_choice = { type: 'any' };
    else if (tc === 'none') out.tool_choice = { type: 'none' };
    else if (tc === 'auto') out.tool_choice = { type: 'auto' };
    else if (tc && tc.type === 'function' && tc.function?.name) {
      out.tool_choice = { type: 'tool', name: tc.function.name };
    }
  }
  if (req.temperature != null) out.temperature = req.temperature;
  return out;
}

/* ---------------- Anthropic 流 → OpenAI SSE ---------------- */
function pipeStream(upstream, res, model) {
  res.writeHead(200, {
    'Content-Type': 'text/event-stream; charset=utf-8',
    'Cache-Control': 'no-cache',
    Connection: 'keep-alive',
    'X-Accel-Buffering': 'no',
  });

  const id = 'chatcmpl-' + Date.now().toString(36);
  const created = Math.floor(Date.now() / 1000);
  const base = { id, object: 'chat.completion.chunk', created, model };

  let toolIndex = -1;
  let blockKinds = new Map(); // anthropic block index -> { kind, toolIndex }
  let promptTokens = 0;
  let completionTokens = 0;
  let finish = 'stop';

  const send = obj => { if (!res.writableEnded) res.write('data: ' + JSON.stringify(obj) + '\n\n'); };
  const sendChoice = (delta, finishReason = null) => send({
    ...base,
    choices: [{ index: 0, delta, finish_reason: finishReason }],
  });

  sendChoice({ role: 'assistant', content: '' });

  let buf = '';
  const onLine = line => {
    if (!line.startsWith('data:')) return;
    const payload = line.slice(5).trim();
    if (!payload) return;
    let ev;
    try { ev = JSON.parse(payload); } catch { return; }

    if (ev.type === 'message_start') {
      promptTokens = ev.message?.usage?.input_tokens || 0;
      return;
    }
    if (ev.type === 'content_block_start') {
      const b = ev.content_block || {};
      if (b.type === 'tool_use') {
        toolIndex++;
        blockKinds.set(ev.index, { kind: 'tool', toolIndex });
        sendChoice({
          tool_calls: [{
            index: toolIndex,
            id: b.id || `call_${toolIndex}`,
            type: 'function',
            function: { name: b.name || 'tool', arguments: '' },
          }],
        });
      } else {
        blockKinds.set(ev.index, { kind: 'text' });
      }
      return;
    }
    if (ev.type === 'content_block_delta') {
      const d = ev.delta || {};
      if (d.type === 'text_delta' && d.text) {
        sendChoice({ content: d.text });
      } else if (d.type === 'input_json_delta') {
        const info = blockKinds.get(ev.index);
        if (info && info.kind === 'tool') {
          sendChoice({
            tool_calls: [{
              index: info.toolIndex,
              function: { arguments: d.partial_json || '' },
            }],
          });
        }
      }
      return;
    }
    if (ev.type === 'message_delta') {
      // 上游把 input_tokens 放在 message_delta 的 usage 里（message_start 是 0）。
      if (ev.usage?.input_tokens) promptTokens = ev.usage.input_tokens;
      if (ev.delta?.stop_reason) {
        finish = ev.delta.stop_reason === 'tool_use' ? 'tool_calls'
          : ev.delta.stop_reason === 'max_tokens' ? 'length' : 'stop';
      }
      if (ev.usage?.output_tokens) completionTokens = ev.usage.output_tokens;
      return;
    }
    if (ev.type === 'message_stop') {
      sendChoice({}, finish);
      send({ ...base, choices: [], usage: {
        prompt_tokens: promptTokens,
        completion_tokens: completionTokens,
        total_tokens: promptTokens + completionTokens,
      } });
      res.write('data: [DONE]\n\n');
      res.end();
      return;
    }
    if (ev.type === 'error') {
      send({ error: openAIError(ev.error?.message || 'upstream error') });
      if (!res.writableEnded) {
        res.write('data: [DONE]\n\n');
        res.end();
      }
    }
  };

  upstream.on('data', chunk => {
    buf += chunk.toString('utf8');
    let idx;
    while ((idx = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, idx).trimEnd();
      buf = buf.slice(idx + 1);
      if (line) onLine(line);
    }
  });
  upstream.on('end', () => {
    if (!res.writableEnded) {
      sendChoice({}, finish);
      res.write('data: [DONE]\n\n');
      res.end();
    }
  });
  upstream.on('error', err => {
    if (res.writableEnded) return;
    // 响应头已经发出去时不能再 writeHead/jsonResponse，只能结束流。
    if (!res.headersSent) {
      return jsonResponse(res, 502, openAIError(err.message));
    }
    send({ error: openAIError(err.message) });
    res.end();
  });
  res.on('close', () => upstream.destroy());
}

/* ---------------- 服务 ---------------- */
const server = http.createServer((req, res) => {
  if (req.method === 'GET' && req.url.endsWith('/models')) {
    return jsonResponse(res, 200, {
      object: 'list',
      data: [{ id: DEFAULT_MODEL, object: 'model', owned_by: 'anthropic-proxy' }],
    });
  }
  if (req.method !== 'POST' || !req.url.includes('/chat/completions')) {
    return jsonResponse(res, 404, openAIError('not found', 'not_found'));
  }
  let raw = '';
  req.on('data', c => { raw += c; });
  req.on('end', () => {
    let body;
    try { body = JSON.parse(raw || '{}'); } catch {
      return jsonResponse(res, 400, openAIError('invalid json', 'invalid_request'));
    }
    const payload = JSON.stringify(toAnthropic(body));
    const up = new URL(UPSTREAM + '/v1/messages');
    const upReq = transport.request(up, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'x-api-key': KEY,
        Authorization: 'Bearer ' + KEY,
        'anthropic-version': '2023-06-01',
        Accept: 'text/event-stream',
      },
    }, upRes => {
      if (upRes.statusCode !== 200) {
        let errBody = '';
        upRes.on('data', c => { errBody += c; });
        upRes.on('end', () => {
          jsonResponse(res, upRes.statusCode || 502, openAIError(errBody.slice(0, 1000)));
        });
        return;
      }
      pipeStream(upRes, res, body.model || DEFAULT_MODEL);
    });
    upReq.on('error', err => jsonResponse(res, 502, openAIError(err.message)));
    upReq.end(payload);
  });
});

const [host, port] = LISTEN.split(':');
server.listen(Number(port), host, () => {
  console.log(`anthropic-proxy listening on http://${LISTEN}/v1 → ${UPSTREAM}`);
});
