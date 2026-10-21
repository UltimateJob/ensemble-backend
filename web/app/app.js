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

  function makeAv(name, color, cls) {
    const ch = (name || '?').slice(0, 1);
    return `<span class="av ${cls || ''}" style="${avatarStyle(name || '', color)}">${esc(ch)}</span>`;
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
    if (new URLSearchParams(location.search).get('scenes')) openScenes();
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
    const box = $('#convList');
    if (!state.convs.length) {
      box.innerHTML = `<div style="padding:24px 12px;text-align:center;font-size:12px;color:var(--tx-4)">
        还没有群聊<br><span style="color:var(--tx-4)">点右上角「新建」开一个</span>
      </div>`;
      return;
    }
    box.innerHTML = state.convs.map(c => {
      const sc = state.scenarios.find(s => s.key === c.studio) || {};
      const color = sc.accent || '#4F46E5';
      return `<div class="conv ${state.cur && state.cur.id === c.id ? 'on' : ''}" data-id="${c.id}">
        ${makeAv(c.title, color)}
        <div class="conv-body">
          <div class="conv-name">${esc(c.title)}</div>
          <div class="conv-meta">${esc(sc.name || c.studio || '自定义')} · ${(c.members || []).length || (sc.agents || []).length} 个角色</div>
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
    state.agents = sc.agents || [];

    renderConvs();
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
      } else {
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

      if (ev.type === 'error') {
        addSysNote(ev.name + ' 出错了：' + ev.content);
        return;
      }
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
    $('#sceneGrid').innerHTML = state.scenarios.map(s => `
      <button class="scene" data-k="${s.key}">
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
      // 开场白
      if (sc.opening) {
        addSysNote('群已建好');
      }
      toast('群聊已创建');
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
