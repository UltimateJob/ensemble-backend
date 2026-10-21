/* ============================================================
   合奏 Ensemble · 页面层
   登录 / 侧栏 / 场景 / 角色编辑器 / 事件绑定
   ============================================================ */

import {
  $, $$, api, state, toast, esc, icon, makeAv, timeStr, hydrateIcons, shift,
} from './core.js';

import {
  loadConvs, renderConvs, openConv, loadEarlier, buildMsgEl, addMsg,
  addSysNote, scrollBottom, renderMembers, renderPipeline,
  addArtifact, appendArtifactCard, send, kickoff,
} from './chat.js';

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
  await loadLibrary();
  await loadConvs();
  renderOnboard();
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
// 让群里继续聊：不需要用户发言，角色自己找话题
$('#nudgeBtn').addEventListener('click', async () => {
  if (state.busy || !state.cur) return;
  const btn = $('#nudgeBtn');
  btn.disabled = true;
  try {
    await kickoff(state.cur.id);
  } finally {
    btn.disabled = false;
  }
});

$('#clearBtn').addEventListener('click', () => {
  if (!state.cur) return;
  $('#msgWrap').innerHTML = '';
  addSysNote('显示已清空（服务端记录保留）');
});

/* ---------------- 上手引导 ---------------- */
// 不弹教程、不遮屏幕。就挂在会话列表下面，
// 把「造角色」「存工作室」这些藏在菜单里的能力变成看得见的下一步。
function renderOnboard() {
  const box = $('#onboard');
  if (!box) return;
  if (localStorage.getItem('ens_onboard_off') === '1') {
    box.innerHTML = '';
    return;
  }

  const steps = [
    {
      done: state.convs.length > 0,
      text: '开一个群',
      hint: '右上角 <em>新建</em>',
    },
    {
      done: state.library.length > 0,
      text: '造一个自己的角色',
      hint: '场景页 <em>自己造角色</em>',
    },
    {
      done: state.scenarios.some(s => s.custom),
      text: '把角色组合存成工作室',
      hint: '群右上 <em>⋯ → 存为工作室</em>',
    },
  ];
  const doneN = steps.filter(s => s.done).length;
  if (doneN === steps.length) {
    box.innerHTML = '';
    return;
  }

  box.innerHTML = `<div class="ob-card">
    <div class="ob-head">
      <b>上手</b>
      <span>${doneN}/${steps.length}</span>
      <button class="ob-close" id="obClose" title="不再显示">×</button>
    </div>
    ${steps.map(s => `
      <div class="ob-item ${s.done ? 'done' : ''}">
        <span class="dot">✓</span>
        <span class="txt">${s.text}${s.done ? '' : '<br><em>' + s.hint + '</em>'}</span>
      </div>`).join('')}
  </div>`;

  const close = $('#obClose');
  if (close) {
    close.addEventListener('click', () => {
      localStorage.setItem('ens_onboard_off', '1');
      box.innerHTML = '';
    });
  }
}

/* ---------------- 角色库 ---------------- */
async function loadLibrary() {
  try {
    state.library = await api('/v1/agents');
  } catch (e) {
    state.library = [];
  }
}

function renderLibPick() {
  const box = $('#libPick');
  if (!box) return;
  if (!state.library.length) {
    box.innerHTML = `<div class="lib-box"><div class="lib-head"><b>我的角色</b></div>
      <div class="lib-empty">还没有存过角色。点下面「自己造角色」，生成后存入角色库，这里就能勾选。</div></div>`;
    return;
  }
  box.innerHTML = `<div class="lib-box">
    <div class="lib-head"><b>我的角色</b>勾选后一起拉进群 <span>${state.picked.length}/${state.library.length}</span></div>
    <div class="lib-row">
      ${state.library.map(a => `
        <button class="lib-chip ${state.picked.includes(a.id) ? 'on' : ''}" data-lib="${a.id}">
          ${makeAv(a.name, a.color)}
          <span>${esc(a.name)}</span>
        </button>`).join('')}
    </div>
  </div>`;
  box.querySelectorAll('[data-lib]').forEach(b => {
    b.addEventListener('click', () => {
      const id = b.dataset.lib;
      const i = state.picked.indexOf(id);
      if (i >= 0) state.picked.splice(i, 1); else state.picked.push(id);
      renderLibPick();
    });
  });
}

/* ---------------- 场景 ---------------- */
const ICONS = {
  briefcase: '<path d="M16 20V4a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v16"/><rect x="2" y="6" width="20" height="14" rx="2"/>',
};

function openScenes() {
  state.pick = null;
  state.picked = [];
  renderLibPick();
  $('#sceneCreate').disabled = true;
  $('#sceneGrid').innerHTML = state.scenarios.map((s, i) => `
    <button class="scene" data-k="${s.key}" style="--i:${i}">
      <span class="scene-ic" style="background:linear-gradient(140deg,${s.accent},${shift(s.accent, -26)})">
        ${icon(s.icon || 'briefcase', 20)}
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
      renderSceneDetail(state.pick);
    });
  });
  // 默认展开第一个，避免用户不知道能干什么
  if (state.scenarios[0]) {
    const first = document.querySelector('.scene');
    if (first) first.click();
  }
  $('#scenes').classList.add('on');
}

/* 移动端抽屉 */
const drawer = $('#drawer'), drawerMask = $('#drawerMask');
function openDrawer(){ drawer.classList.add('on'); drawerMask.classList.add('on'); }
function closeDrawer(){ drawer.classList.remove('on'); drawerMask.classList.remove('on'); }
$('#drawerBtn').addEventListener('click', openDrawer);
drawerMask.addEventListener('click', closeDrawer);
$('#drawerNew').addEventListener('click', () => { closeDrawer(); openScenes(); });

// 场景详情：角色 + 示例输入
const TRY = {
  bid: ['华东医院数据中心投标，15 天截止，要 ISO 27001', '帮我把这份招标文件拆成条目表', '这个标有没有废标风险？'],
  code: ['做个功能：上传 Excel 自动生成图表并导出 PNG', '这个模块该怎么拆？', '帮我看看这段逻辑的边界情况'],
  research: ['分析 2024 年国内储能行业的装机量和增速', '这个数字的来源可靠吗？', '帮我搭一个分析框架'],
  anime: ['今天好无聊，来点有意思的', '你们觉得加班值得吗？', '介绍下你们自己'],
  companion: ['今天加班到十点，有点累', '最近有点迷茫', '周末干点什么好'],
};

function renderSceneDetail(key) {
  const sc = state.scenarios.find(s => s.key === key);
  const box = $('#sceneDetail');
  if (!sc) { box.innerHTML = ''; return; }
  const chips = (TRY[key] || []).map(t =>
    `<button class="sd-chip" data-try="${esc(t)}">${esc(t)}</button>`).join('');
  box.innerHTML = `
    <div class="sd-head"><b>${esc(sc.name)}</b>${esc(sc.tagline)}</div>
    <div class="sd-agents">
      ${(sc.agents || []).map(a => `
        <div class="sd-agent">
          ${makeAv(a.name, a.color)}
          <div>
            <div class="sd-an">${esc(a.name)}</div>
            <div class="sd-at">${esc(a.title)}</div>
          </div>
        </div>`).join('')}
    </div>
    ${chips ? `<div class="sd-try"><div class="sd-try-h">可以这样开场 · 点一下直接建群后填进输入框</div>
      <div class="sd-chips">${chips}</div></div>` : ''}`;

  box.querySelectorAll('[data-try]').forEach(b => {
    b.addEventListener('click', () => {
      state.pendingText = b.dataset.try;
      $('#sceneCreate').click();
    });
  });
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
    // 把勾选的库角色复制进这个群（快照，之后改库不影响）
    const fromLib = state.picked
      .map(id => state.library.find(a => a.id === id))
      .filter(Boolean)
      .map(a => ({ key: 'lib_' + a.id, name: a.name, title: a.title, emoji: a.emoji, color: a.color, prompt: a.prompt }));

    const conv = await api('/v1/conversations', {
      method: 'POST',
      body: {
        title: title || sc.name,
        studio: fromLib.length && !(sc.agents || []).length ? 'custom' : sc.key,
        members: (sc.agents || []).map(a => a.key),
        agents: fromLib.length ? fromLib : undefined,
      },
    });
    $('#scenes').classList.remove('on');
    await loadConvs();
    await openConv(conv.id);
    toast('群聊已创建');
    if (state.pendingText) {
      const ta2 = $('#ta');
      ta2.value = state.pendingText;
      ta2.dispatchEvent(new Event('input', { bubbles: true }));
      state.pendingText = null;
      ta2.focus();
    }
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

hydrateIcons();
boot();

// 供 chat.js 反向刷新引导清单
window.__refreshOnboard = renderOnboard;

// 调试出口：出问题时可以在控制台直接调
window.__ens = { kickoff, send, openConv, loadConvs, state, renderOnboard };
