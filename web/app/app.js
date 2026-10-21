/* ============================================================
   合奏 Ensemble · 客户端逻辑
   原生 JS，无框架。SSE 走 fetch + ReadableStream。
   ============================================================ */
(function () {
  'use strict';

  const $ = s => document.querySelector(s);
  const $$ = s => [...document.querySelectorAll(s)];
  const API = '';

  const urlToken = new URLSearchParams(location.search).get('token');
  if (urlToken) localStorage.setItem('ens_token', urlToken);

  const state = {
    token: urlToken || localStorage.getItem('ens_token') || '',
    user: null,
    scenarios: [],
    convs: [],
    cur: null,        // 当前会话
    agents: [],       // 当前场景角色
    busy: false,
    pick: null,       // 场景选择中
  };

  /* ---------------- 工具 ---------------- */
  function toast(msg, ms) {
    const t = $('#toast');
    t.textContent = msg;
    t.classList.add('on');
    clearTimeout(t._t);
    t._t = setTimeout(() => t.classList.remove('on'), ms || 1800);
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function timeStr(d) {
    const x = new Date(d);
    if (isNaN(x)) return '';
    return x.getHours().toString().padStart(2, '0') + ':' + x.getMinutes().toString().padStart(2, '0');
  }

  function agoStr(d) {
    const diff = (Date.now() - new Date(d).getTime()) / 1000;
    if (diff < 60) return '刚刚';
    if (diff < 3600) return Math.floor(diff / 60) + ' 分钟前';
    if (diff < 86400) return Math.floor(diff / 3600) + ' 小时前';
    return Math.floor(diff / 86400) + ' 天前';
  }

  // 由名字生成稳定的渐变头像
  function avatarStyle(seed, color) {
    let h = 0;
    for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
    const hue = h % 360;
    if (color) {
      return `background:linear-gradient(140deg, ${color}, ${shift(color, -22)})`;
    }
    return `background:linear-gradient(140deg, hsl(${hue} 62% 58%), hsl(${(hue + 40) % 360} 62% 46%))`;
  }

  function shift(hex, amt) {
    const m = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex || '');
    if (!m) return hex;
    const c = [1, 2, 3].map(i => Math.max(0, Math.min(255, parseInt(m[i], 16) + amt)));
    return '#' + c.map(v => v.toString(16).padStart(2, '0')).join('');
  }

  /* 由名字生成稳定的几何头像：背景 + 2~3 个图形。
     思路参考 Boring Avatars / DiceBear，但不依赖任何外部资源。 */
  function hashStr(str) {
    let h = 2166136261;
    for (let i = 0; i < str.length; i++) {
      h ^= str.charCodeAt(i);
      h = Math.imul(h, 16777619);
    }
    return h >>> 0;
  }

  function rng(seed) {
    let s = seed;
    return function () {
      s ^= s << 13; s >>>= 0;
      s ^= s >> 17;
      s ^= s << 5; s >>>= 0;
      return s / 4294967296;
    };
  }

  function geoAvatar(name, color) {
    const seed = hashStr(name || 'x');
    const r = rng(seed);
    const pick = arr => arr[Math.floor(r() * arr.length)];

    // 基色：角色色优先，否则由名字推导
    let base = color;
    if (!base) {
      const hue = seed % 360;
      base = `hsl(${hue} 58% 54%)`;
    }
    const tints = [0, 18, -22, 40];
    const t = tints[Math.floor(r() * tints.length)];
    const bg = base;
    const fg = `rgba(255,255,255,.85)`;
    const fg2 = `rgba(255,255,255,.42)`;

    const variant = Math.floor(r() * 4);
    let art = '';

    // 角落装饰，不影响首字可读性
    if (variant === 0) {
      art = `<circle cx="34" cy="6" r="12" fill="rgba(255,255,255,.16)"/>`;
    } else if (variant === 1) {
      art = `<path d="M0 40 L40 14 L40 40 Z" fill="rgba(255,255,255,.16)"/>`;
    } else if (variant === 2) {
      art = `<circle cx="6" cy="34" r="11" fill="rgba(255,255,255,.14)"/>`;
    } else {
      art = `<rect x="26" y="-6" width="22" height="22" rx="6" fill="rgba(255,255,255,.15)" transform="rotate(20 37 5)"/>`;
    }
    const ch = (name || '?').trim().slice(0, 1);
    art += `<text x="20" y="21" text-anchor="middle" dominant-baseline="central"
      font-family="-apple-system,BlinkMacSystemFont,PingFang SC,sans-serif"
      font-size="17" font-weight="600" fill="rgba(255,255,255,.96)">${ch.replace(/[<>&]/g, '')}</text>`;

    return `<svg viewBox="0 0 40 40" xmlns="http://www.w3.org/2000/svg" stroke="none" style="display:block;width:100%;height:100%">
      <defs><linearGradient id="g${seed}" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0%" stop-color="${bg}"/>
        <stop offset="100%" stop-color="${shift(hexOf(bg), -26) || bg}"/>
      </linearGradient></defs>
      <rect width="40" height="40" fill="url(#g${seed})"/>
      ${art}
    </svg>`;
  }

  // 把 hsl 或 hex 统一成 hex，供 shift 使用
  function hexOf(c) {
    if (/^#/.test(c)) return c;
    const m = /hsl\((\d+)/.exec(c);
    if (!m) return '#4F46E5';
    const h = +m[1] / 360;
    const s = 0.58, l = 0.54;
    const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
    const p = 2 * l - q;
    const hue2rgb = (t) => {
      if (t < 0) t += 1;
      if (t > 1) t -= 1;
      if (t < 1 / 6) return p + (q - p) * 6 * t;
      if (t < 1 / 2) return q;
      if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
      return p;
    };
    const R = Math.round(hue2rgb(h + 1 / 3) * 255);
    const G = Math.round(hue2rgb(h) * 255);
    const B = Math.round(hue2rgb(h - 1 / 3) * 255);
    return '#' + [R, G, B].map(v => v.toString(16).padStart(2, '0')).join('');
  }

  function makeAv(name, color, cls) {
    const c = color || autoColor(name || '');
    const base = hexOf(c);
    return `<span class="av ${cls || ''}" style="background:linear-gradient(140deg,${base},${shift(base, -30)})"><b>${esc((name || '?').trim().slice(0, 1))}</b></span>`;
  }

  // 没有角色色时，由名字推导一个稳定色
  function autoColor(name) {
    let h = 0;
    for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
    return `hsl(${h % 360} 56% 52%)`;
  }

  /* ---------------- API ---------------- */
  async function api(path, opt) {
    opt = opt || {};
    const headers = { 'Content-Type': 'application/json' };
    if (state.token) headers['Authorization'] = 'Bearer ' + state.token;
    const res = await fetch(API + path, {
      method: opt.method || 'GET',
      headers,
      body: opt.body ? JSON.stringify(opt.body) : undefined,
    });
    let json = {};
    try { json = await res.json(); } catch (e) { /* ignore */ }
    if (!res.ok || json.ok === false) {
      const msg = (json.error && json.error.message) || ('HTTP ' + res.status);
      const err = new Error(msg);
      err.status = res.status;
      err.code = json.error && json.error.code;
      throw err;
    }
    return json.data;
  }

  /* ---------------- 登录 ---------------- */
  let segKind = 'phone';

  $$('#seg button').forEach(b => {
    b.addEventListener('click', () => {
      $$('#seg button').forEach(x => x.classList.remove('on'));
      b.classList.add('on');
      segKind = b.dataset.k;
      $('#form-phone').style.display = segKind === 'phone' ? '' : 'none';
      $('#form-email').style.display = segKind === 'email' ? '' : 'none';
    });
  });

  function bindSendCode(btnSel, inputSel) {
    const btn = $(btnSel), inp = $(inputSel);
    btn.addEventListener('click', async () => {
      const target = inp.value.trim();
      if (!target) return toast('先填手机号或邮箱');
      btn.disabled = true;
      try {
        const d = await api('/v1/auth/send-code', {
          method: 'POST', body: { kind: segKind, target }
        });
        toast('验证码已发送：' + (d.devCode || ''));
        const codeInput = segKind === 'phone' ? $('#code') : $('#code2');
        if (d.devCode) codeInput.value = d.devCode;
        let n = 60;
        btn.textContent = n + 's';
        const t = setInterval(() => {
          n--;
          btn.textContent = n + 's';
          if (n <= 0) { clearInterval(t); btn.disabled = false; btn.textContent = '获取验证码'; }
        }, 1000);
      } catch (e) {
        btn.disabled = false;
        showLoginErr(e.message);
      }
    });
  }

  bindSendCode('#sendCode', '#phone');
  bindSendCode('#sendCode2', '#email');

  function showLoginErr(msg) {
    const el = $('#loginErr');
    el.textContent = msg;
    el.style.display = msg ? 'block' : 'none';
  }

  async function doLogin() {
    const target = segKind === 'phone' ? $('#phone').value.trim() : $('#email').value.trim();
    const code = segKind === 'phone' ? $('#code').value.trim() : $('#code2').value.trim();
    if (!target) return showLoginErr('请填写手机号或邮箱');
    showLoginErr('');
    const btn = $('#doLogin');
    btn.disabled = true;
    btn.textContent = '登录中…';
    try {
      const d = await api('/v1/auth/login', {
        method: 'POST',
        body: { kind: segKind, target, code },
      });
      state.token = d.token;
      state.user = d.user;
      localStorage.setItem('ens_token', d.token);
      enterApp();
    } catch (e) {
      showLoginErr(e.message);
      btn.disabled = false;
      btn.textContent = '登录 / 注册';
    }
  }

  $('#doLogin').addEventListener('click', doLogin);
  ['#phone', '#code', '#email', '#code2'].forEach(s => {
    $(s).addEventListener('keydown', e => { if (e.key === 'Enter') doLogin(); });
  });

  /* ---------------- 启动 ---------------- */
  async function boot() {
    if (!state.token) return;
    try {
      const d = await api('/v1/me');
      state.user = d.user;
      enterApp();
    } catch (e) {
      localStorage.removeItem('ens_token');
      state.token = '';
    }
  }

  async function enterApp() {
    $('#login').style.display = 'none';
    $('#app').classList.add('on');
    try {
      state.scenarios = await api('/v1/scenarios');
    } catch (e) {
      state.scenarios = [];
    }
    await loadConvs();
    const qs2 = new URLSearchParams(location.search);
    if (qs2.get('scenes') || qs2.get('forge')) openScenes();
    if (qs2.get('forge')) {
      $('#forge').classList.add('on');
      renderForge();
      if (qs2.get('name')) {
        $('#fName').value = qs2.get('name');
        $('#fKeys').value = qs2.get('keys') || '';
        setTimeout(() => $('#fMake').click(), 200);
      }
    }
    const wantConv = new URLSearchParams(location.search).get('c');
    if (wantConv && state.convs.some(c => c.id === wantConv)) {
      openConv(wantConv);
    }
  }

  /* ---------------- 会话 ---------------- */
  async function loadConvs() {
    try {
      state.convs = await api('/v1/conversations');
    } catch (e) {
      state.convs = [];
    }
    renderConvs();
  }

  function renderConvs() {
    renderInto($('#convList'));
    renderInto($('#drawerList'));
  }

  function renderInto(box) {
    if (!box) return;
    if (!state.convs.length) {
      box.innerHTML = `<div style="padding:24px 12px;text-align:center;font-size:12px;color:var(--tx-4)">
        还没有群聊<br><span style="color:var(--tx-4)">点「新建」开一个</span>
      </div>`;
      return;
    }
    box.innerHTML = state.convs.map(c => {
      const sc = state.scenarios.find(s => s.key === c.studio) || {};
      const color = sc.accent || (c.agents && c.agents[0] && c.agents[0].color) || '#4F46E5';
      return `<div class="conv ${state.cur && state.cur.id === c.id ? 'on' : ''}" data-id="${c.id}">
        ${makeAv(c.title, color)}
        <div class="conv-body">
          <div class="conv-name">${esc(c.title)}</div>
          <div class="conv-meta">${esc(sc.name || (c.agents && c.agents.length ? '自定义群' : c.studio) || '自定义')} · ${(c.agents || []).length || (c.members || []).length || (sc.agents || []).length} 个角色</div>
        </div>
        <div class="conv-time">${agoStr(c.updatedAt || c.createdAt).replace('前', '')}</div>
      </div>`;
    }).join('');

    $$('.conv').forEach(el => {
      el.addEventListener('click', () => openConv(el.dataset.id));
    });
  }

  async function openConv(id) {
    if (state.busy) return toast('上一轮还没结束');
    const conv = state.convs.find(c => c.id === id);
    if (!conv) return;
    state.cur = conv;

    const sc = state.scenarios.find(s => s.key === conv.studio) || { agents: [] };
    // 自定义角色优先
    state.agents = (conv.agents && conv.agents.length)
      ? conv.agents
      : (sc.agents || []);

    renderConvs();
    if (drawer.classList.contains('on')) closeDrawer();
    $('#chatEmpty').style.display = 'none';
    $('#chatHead').style.display = 'flex';
    $('#msgs').style.display = 'block';
    $('#composer').style.display = 'block';
    $('#chatTitle').textContent = conv.title;
    $('#crumbName').textContent = conv.title;
    renderMembers();
    renderPipeline(true);
    $('#msgWrap').innerHTML = '';

    try {
      const msgs = await api('/v1/conversations/' + id + '/messages?limit=100');
      artifacts.length = 0;
      msgs.forEach(m => {
        const agent = state.agents.find(a => a.name === m.agent);
        addMsg({
          name: m.role === 'user' ? (state.user && state.user.name) || '你' : (m.agent || 'Agent'),
          title: agent ? agent.title : '',
          color: agent ? agent.color : null,
          text: m.content,
          me: m.role === 'user',
          time: m.createdAt,
        });
        // 有产物定义的角色，历史里也补一张文件卡
        if (agent && agent.artifact && m.role === 'assistant') {
          addArtifact(agent.name, agent.artifact, m.content);
          appendArtifactCard(agent.artifact, m.content.slice(0, 28));
        }
      });
      if (!msgs.length) {
        addSysNote('群已建好，说第一句话试试');
      } else if (msgs.length > 1) {
        pipeDone = state.agents.length;
        renderPipeline(false);
      }
    } catch (e) {
      toast(e.message);
    }
    scrollBottom();
  }

  /* ---------------- 渲染 ---------------- */
  function addMsg(o) {
    const wrap = $('#msgWrap');
    const el = document.createElement('div');
    el.className = 'msg' + (o.me ? ' me' : '');
    el.innerHTML = `
      ${o.me ? '' : makeAv(o.name, o.color)}
      <div class="msg-body">
        <div class="msg-head">
          <span class="msg-name" ${o.color ? `style="color:${o.color}"` : ''}>${esc(o.name)}</span>
          ${o.title ? `<span class="msg-title">${esc(o.title)}</span>` : ''}
          <span class="msg-time">${o.time ? timeStr(o.time) : ''}</span>
        </div>
        <div class="msg-text"></div>
      </div>`;
    el.querySelector('.msg-text').textContent = o.text || '';
    wrap.appendChild(el);
    return el;
  }

  function addSysNote(text) {
    const el = document.createElement('div');
    el.className = 'sys-note';
    el.textContent = text;
    $('#msgWrap').appendChild(el);
    return el;
  }

  function scrollBottom() {
    const box = $('#msgs');
    if (box) box.scrollTop = box.scrollHeight;
  }

  function renderMembers() {
    const box = $('#memberList');
    box.innerHTML = state.agents.map(a => `
      <div class="member">
        ${makeAv(a.name, a.color, 'av-sm')}
        <div>
          <div class="mn">${esc(a.name)}</div>
          <div class="mt">${esc(a.title)}</div>
        </div>
        <span class="st"></span>
      </div>`).join('');
    $('#memberCount').textContent = state.agents.length;
    $('#topAvatars').innerHTML = state.agents.slice(0, 5)
      .map(a => makeAv(a.name, a.color, 'av-sm')).join('');
  }

  let pipeDone = 0;
  function renderPipeline(reset) {
    if (reset) pipeDone = 0;
    const box = $('#pipeline');
    if (!box) return;
    box.innerHTML = state.agents.map((a, i) => {
      const cls = i < pipeDone ? 'done' : (i === pipeDone ? 'live' : '');
      return `<div class="pipe-step ${cls}">
        <span class="n">${i + 1}</span>
        <span>${esc(a.name)}</span>
        <span class="r">${i < pipeDone ? '✓' : ''}</span>
      </div>`;
    }).join('');
    $('#pipeCount').textContent = pipeDone + '/' + state.agents.length;
  }

  const artifacts = [];

  function buildArtifactCard(title, detail) {
    const card = document.createElement('div');
    card.className = 'artifact';
    card.innerHTML = `
      <span class="fi">
        <svg class="ic" viewBox="0 0 24 24"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/></svg>
      </span>
      <div>
        <div class="an">${esc(title)}</div>
        <div class="ad">${esc(detail || '')}</div>
      </div>
      <span class="go"><svg class="ic" viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg></span>`;
    card.addEventListener('click', () => toast('打开 ' + title));
    return card;
  }

  function appendArtifactCard(title, detail) {
    $('#msgWrap').appendChild(buildArtifactCard(title, detail));
  }

  function addArtifact(name, title, detail) {
    artifacts.push({ name, title, detail });
    $('#artifactCount').textContent = artifacts.length;
    const box = $('#artifactList');
    box.innerHTML = artifacts.map(a => `
      <div style="display:flex;align-items:center;gap:8px;padding:5px 0;color:var(--tx-2)">
        <svg class="ic" style="width:13px;height:13px;color:var(--pri)" viewBox="0 0 24 24">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/>
        </svg>
        <span style="font-size:12px">${esc(a.title)}</span>
      </div>`).join('');
  }

  /* ---------------- 发送（核心） ---------------- */
  async function send() {
    if (state.busy || !state.cur) return;
    const ta = $('#ta');
    const text = ta.value.trim();
    if (!text) return;

    ta.value = '';
    ta.style.height = 'auto';
    $('#sendBtn').disabled = true;
    state.busy = true;

    addMsg({ name: (state.user && state.user.name) || '你', text, me: true, time: new Date() });
    scrollBottom();

    renderPipeline(true);
    const container = document.createElement('div');
    $('#msgWrap').appendChild(container);

    let curAgent = null;
    let curEl = null;
    let curText = '';

    try {
      const res = await fetch(API + '/v1/conversations/' + state.cur.id + '/orchestrate', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + state.token,
        },
        body: JSON.stringify({ content: text }),
      });

      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        throw new Error((j.error && j.error.message) || 'HTTP ' + res.status);
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });

        const parts = buf.split('\n\n');
        buf = parts.pop();

        for (const part of parts) {
          const line = part.trim();
          if (!line.startsWith('data:')) continue;
          let ev;
          try { ev = JSON.parse(line.slice(5).trim()); } catch (e) { continue; }
          handleEvent(ev);
        }
      }
    } catch (e) {
      addSysNote('出错了：' + e.message);
    } finally {
      state.busy = false;
      if (curEl) curEl.querySelector('.msg-text').classList.remove('caret');
      scrollBottom();
      loadConvs();
    }

    function handleEvent(ev) {
      if (ev.type === 'start') {
        curAgent = ev;
        curText = '';
        container.querySelectorAll('.msg.speaking').forEach(m => m.classList.remove('speaking'));
        // 思考占位
        curEl = document.createElement('div');
        curEl.className = 'msg';
        curEl.innerHTML = `
          ${makeAv(ev.name, ev.color)}
          <div class="msg-body">
            <div class="msg-head">
              <span class="msg-name" style="color:${ev.color}">${esc(ev.name)}</span>
              <span class="msg-time"></span>
            </div>
            <div class="thinking"><i></i><i></i><i></i></div>
            <div class="msg-text" style="display:none"></div>
          </div>`;
        container.appendChild(curEl);
        curEl.classList.add('speaking');
        scrollBottom();
        return;
      }

      if (ev.type === 'delta' && curEl) {
        curEl.querySelector('.thinking').style.display = 'none';
        const tx = curEl.querySelector('.msg-text');
        tx.style.display = '';
        curText += ev.content;
        tx.textContent = curText;
        tx.classList.add('caret');
        scrollBottom();
        return;
      }

      if (ev.type === 'end' && curEl) {
        curEl.classList.remove('speaking');
        const tx = curEl.querySelector('.msg-text');
        tx.classList.remove('caret');
        curEl.querySelector('.msg-time').textContent = timeStr(new Date());
        pipeDone++;
        renderPipeline(false);
        return;
      }

      if (ev.type === 'artifact') {
        addArtifact(ev.name, ev.title, ev.detail);
        container.appendChild(buildArtifactCard(ev.title, ev.detail));
        scrollBottom();
        return;
      }

      if (ev.type === 'pass') {
        // 有人选择不说话：留一条极轻的痕迹，不占气泡
        const n = document.createElement('div');
        n.className = 'pass-note';
        n.textContent = ev.name + ' 没接话';
        container.appendChild(n);
        return;
      }

      if (ev.type === 'ask') {
        if (curEl) curEl.classList.add('asking');
        return;
      }

      if (ev.type === 'error') {
        addSysNote(ev.name + ' 出错了：' + ev.content);
        return;
      }
    }
  }

  /* ---------------- 自主开场 ---------------- */
  async function kickoff(convId) {
    if (state.busy) return;
    state.busy = true;
    $('#sendBtn').disabled = true;
    const container = document.createElement('div');
    $('#msgWrap').appendChild(container);
    renderPipeline(true);

    let curEl = null, curText = '';

    try {
      const res = await fetch(API + '/v1/conversations/' + convId + '/kickoff', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + state.token },
        body: JSON.stringify({}),
      });
      if (!res.ok) return;
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const parts = buf.split('\n\n');
        buf = parts.pop();
        for (const part of parts) {
          const line = part.trim();
          if (!line.startsWith('data:')) continue;
          let ev; try { ev = JSON.parse(line.slice(5).trim()); } catch (e) { continue; }
          if (ev.type === 'start') {
            curText = '';
            curEl = document.createElement('div');
            curEl.className = 'msg';
            curEl.innerHTML = `
              ${makeAv(ev.name, ev.color)}
              <div class="msg-body">
                <div class="msg-head">
                  <span class="msg-name" style="color:${ev.color}">${esc(ev.name)}</span>
                  <span class="msg-time"></span>
                </div>
                <div class="thinking"><i></i><i></i><i></i></div>
                <div class="msg-text" style="display:none"></div>
              </div>`;
            container.appendChild(curEl);
            curEl.classList.add('speaking');
            scrollBottom();
          } else if (ev.type === 'delta' && curEl) {
            curEl.querySelector('.thinking').style.display = 'none';
            const tx = curEl.querySelector('.msg-text');
            tx.style.display = '';
            curText += ev.content;
            tx.textContent = curText;
            tx.classList.add('caret');
            scrollBottom();
          } else if (ev.type === 'end' && curEl) {
            curEl.classList.remove('speaking');
            curEl.querySelector('.msg-text').classList.remove('caret');
            curEl.querySelector('.msg-time').textContent = timeStr(new Date());
          } else if (ev.type === 'pass') {
            const n = document.createElement('div');
            n.className = 'pass-note';
            n.textContent = ev.name + ' 没接话';
            container.appendChild(n);
            scrollBottom();
          }
        }
      }
    } catch (e) {
      /* 静默失败：开场不重要 */
    } finally {
      state.busy = false;
      $('#sendBtn').disabled = !ta.value.trim();
      scrollBottom();
      loadConvs();
    }
  }

  /* ---------------- 输入框 ---------------- */
  const ta = $('#ta');
  ta.addEventListener('input', () => {
    ta.style.height = 'auto';
    ta.style.height = Math.min(ta.scrollHeight, 140) + 'px';
    $('#sendBtn').disabled = !ta.value.trim() || state.busy;
  });
  ta.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (!state.busy && ta.value.trim()) send();
    }
  });
  $('#sendBtn').addEventListener('click', send);
  $('#attachBtn').addEventListener('click', () => toast('附件上传待接入 · 支持 PDF / DOCX / XLSX'));
  $('#mentionBtn').addEventListener('click', () => {
    if (!state.agents.length) return;
    ta.value += '@' + state.agents[0].name + ' ';
    ta.focus();
  });
  $('#clearBtn').addEventListener('click', () => {
    if (!state.cur) return;
    $('#msgWrap').innerHTML = '';
    addSysNote('显示已清空（服务端记录保留）');
  });

  /* ---------------- 场景 ---------------- */
  const ICONS = {
    briefcase: '<path d="M16 20V4a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v16"/><rect x="2" y="6" width="20" height="14" rx="2"/>',
    code: '<path d="m16 18 6-6-6-6M8 6l-6 6 6 6"/>',
    chart: '<path d="M3 3v18h18"/><path d="m7 15 4-4 3 3 5-6"/>',
    sparkles: '<path d="m12 3 1.9 5.6L19.5 10l-5.6 1.9L12 17.5l-1.9-5.6L4.5 10l5.6-1.4z"/><path d="M19 15l.7 2.1 2.1.7-2.1.7L19 21l-.7-2.5-2.1-.7 2.1-.7z"/>',
    heart: '<path d="M19 14c1.5-1.5 3-3.3 3-5.5A4.5 4.5 0 0 0 12 5 4.5 4.5 0 0 0 2 8.5c0 2.2 1.5 4 3 5.5l7 7z"/>',
  };

  function openScenes() {
    state.pick = null;
    $('#sceneCreate').disabled = true;
    $('#sceneGrid').innerHTML = state.scenarios.map((s, i) => `
      <button class="scene" data-k="${s.key}" style="--i:${i}">
        <span class="scene-ic" style="background:linear-gradient(140deg,${s.accent},${shift(s.accent, -26)})">
          <svg class="ic-20" viewBox="0 0 24 24">${ICONS[s.icon] || ICONS.briefcase}</svg>
        </span>
        <span style="min-width:0;flex:1">
          <span class="scene-n">${esc(s.name)}</span>
          <span class="scene-d">${esc(s.tagline)}</span>
          <span class="scene-m">${(s.agents || []).slice(0, 4).map(a => makeAv(a.name, a.color)).join('')}</span>
        </span>
      </button>`).join('');

    $$('.scene').forEach(el => {
      el.addEventListener('click', () => {
        $$('.scene').forEach(x => x.classList.remove('on'));
        el.classList.add('on');
        state.pick = el.dataset.k;
        $('#sceneCreate').disabled = false;
      });
    });
    $('#scenes').classList.add('on');
  }

  /* 移动端抽屉 */
  const drawer = $('#drawer'), drawerMask = $('#drawerMask');
  function openDrawer(){ drawer.classList.add('on'); drawerMask.classList.add('on'); }
  function closeDrawer(){ drawer.classList.remove('on'); drawerMask.classList.remove('on'); }
  $('#drawerBtn').addEventListener('click', openDrawer);
  drawerMask.addEventListener('click', closeDrawer);
  $('#drawerNew').addEventListener('click', () => { closeDrawer(); openScenes(); });

  $('#newConvBtn').addEventListener('click', openScenes);
  $('#newConvBtn2').addEventListener('click', openScenes);
  $('#startBtn').addEventListener('click', openScenes);
  $('#sceneCancel').addEventListener('click', () => $('#scenes').classList.remove('on'));

  $('#sceneCreate').addEventListener('click', async () => {
    const sc = state.scenarios.find(s => s.key === state.pick);
    if (!sc) return;
    const title = prompt('给这个群起个名字', sc.name);
    if (title === null) return;
    try {
      const conv = await api('/v1/conversations', {
        method: 'POST',
        body: { title: title || sc.name, studio: sc.key, members: (sc.agents || []).map(a => a.key) },
      });
      $('#scenes').classList.remove('on');
      await loadConvs();
      await openConv(conv.id);
      toast('群聊已创建');
      // 建完就让角色自己聊起来，用户进来时群里是活的
      setTimeout(() => kickoff(conv.id), 400);
    } catch (e) {
      toast(e.message);
    }
  });

  /* ---------------- 角色编辑器 ---------------- */
  const forged = [];

  function renderForge() {
    const box = $('#forgeList');
    if (!forged.length) {
      box.innerHTML = `<div style="padding:16px;text-align:center;font-size:12px;color:var(--tx-4)">
        还没有角色。填个名字，让它自己长出来。
      </div>`;
    } else {
      box.innerHTML = forged.map((a, i) => `
        <div class="fcard">
          ${makeAv(a.name, a.color)}
          <div style="min-width:0;flex:1">
            <div class="fcard-n">${esc(a.name)}</div>
            <div class="fcard-t">${esc(a.title)}</div>
            <div class="fcard-p">${esc(a.prompt || '')}</div>
          </div>
          <button class="x" data-i="${i}">
            <svg class="ic" viewBox="0 0 24 24"><path d="M18 6 6 18M6 6l12 12"/></svg>
          </button>
        </div>`).join('');
      box.querySelectorAll('[data-i]').forEach(b => {
        b.addEventListener('click', () => {
          forged.splice(+b.dataset.i, 1);
          renderForge();
        });
      });
    }
    $('#forgeCount').textContent = forged.length ? forged.length + ' 个角色' : '还没有角色';
    $('#forgeDone').disabled = forged.length < 1;
  }

  $('#openForge').addEventListener('click', () => {
    $('#forge').classList.add('on');
    renderForge();
  });
  $('#forgeClose').addEventListener('click', () => $('#forge').classList.remove('on'));
  $('#forge').addEventListener('click', e => {
    if (e.target.id === 'forge') $('#forge').classList.remove('on');
  });

  let making = false;
  $('#fMake').addEventListener('click', async () => {
    if (making) return;
    const name = $('#fName').value.trim();
    const keys = $('#fKeys').value.trim();
    if (!name) return toast('先给角色起个名字');

    making = true;
    const btn = $('#fMake');
    btn.disabled = true;
    btn.textContent = '生成中…';
    try {
      const a = await api('/v1/agents/draft', {
        method: 'POST',
        body: { name, keywords: keys },
      });
      forged.push(a);
      renderForge();
      $('#fName').value = '';
      $('#fKeys').value = '';
      toast(a.name + ' 已生成');
    } catch (e) {
      toast(e.message);
    } finally {
      making = false;
      btn.disabled = false;
      btn.textContent = '生成人设';
    }
  });

  // 回车即生成
  ['#fName', '#fKeys'].forEach(sel => {
    $(sel).addEventListener('keydown', e => {
      if (e.key === 'Enter') $('#fMake').click();
    });
  });

  $('#forgeDone').addEventListener('click', async () => {
    if (!forged.length) return;
    const title = prompt('给这个群起个名字', forged.map(a => a.name).join('、'));
    if (title === null) return;
    try {
      const conv = await api('/v1/conversations', {
        method: 'POST',
        body: {
          title: title || '自定义群',
          studio: 'custom',
          agents: forged.map(a => ({
            key: a.key, name: a.name, title: a.title,
            emoji: a.emoji, color: a.color, prompt: a.prompt,
          })),
        },
      });
      $('#forge').classList.remove('on');
      $('#scenes').classList.remove('on');
      await loadConvs();
      await openConv(conv.id);
      toast('群聊已创建');
      setTimeout(() => kickoff(conv.id), 400);
    } catch (e) {
      toast(e.message);
    }
  });

  /* ---------------- 其他 ---------------- */
  $('#logoutBtn').addEventListener('click', () => {
    localStorage.removeItem('ens_token');
    location.reload();
  });
  $('#searchBtn').addEventListener('click', () => toast('全局搜索待接入'));
  document.addEventListener('keydown', e => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      $('#searchBtn').click();
    }
  });

  boot();
})();
