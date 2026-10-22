/* ============================================================
   合奏 Ensemble · 页面层
   登录 / 侧栏 / 场景 / 角色编辑器 / 事件绑定
   ============================================================ */

import {
  $, $$, api, state, toast, esc, icon, makeAv, fullTimeStr, hydrateIcons, shift, hexOf,
  isOnline, onNetChange, humanError } from './core.js';

import {
  openConv, loadEarlier, buildMsgEl, addMsg,
  addSysNote, scrollBottom, renderMembers, renderPipeline,
  addArtifact, appendArtifactCard, send, kickoff, stopRound,
} from './chat.js';
import { loadConvs, renderConvs, openCtxMenu, closeCtxMenu, setConvOpener } from './convs.js';
import { uiConfirm, uiPrompt } from './dialog.js';
import { initMemory } from './memory.js';
import { initStudio, openStudio } from './studio.js';

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
      showLoginErr(humanError(e));
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
    // 拉一次 /me 拿 demo / upstream——顶栏的「演示」徽章靠它
    try { state.me = await api('/v1/me'); } catch (e) { state.me = null; }
    enterApp();
  } catch (e) {
    showLoginErr(humanError(e));
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
    state.me = d;      // 含 demo / upstream
    enterApp();
  } catch (e) {
    // 只有令牌真的失效才清掉。
    // 之前是不管什么错都清——网络抖一下、后端重启一下，
    // 用户就被登出了，而且没有任何提示，看起来像「又要重新登录」。
    const code = (e && e.code) || '';
    const status = (e && e.status) || 0;
    const authed = code === 'unauthorized' || status === 401 || /HTTP 401/.test(String(e && e.message));
    if (authed) {
      localStorage.removeItem('ens_token');
      state.token = '';
    } else {
      // 连不上：保留令牌，给一个可以重试的提示
      showBootFail(e);
    }
  }
}

// 把 fetch 的原始错误翻成人话。「Failed to fetch」对用户没有任何意义。
function bootFailReason(err) {
  const m = String((err && err.message) || '');
  if (/Failed to fetch|NetworkError|load failed/i.test(m)) return '网络不通，或者服务正在重启。检查一下网络再试。';
  if (/timeout|timed out/i.test(m)) return '服务器响应超时。稍等一下再试。';
  if (/HTTP 5\d\d/.test(m)) return '服务端出错了。稍后再试，或者看看服务日志。';
  return m || '网络可能不通';
}

// 启动连不上服务器时的状态：不伪装成「未登录」，给一句实话和一个重试。
function showBootFail(err) {
  const box = document.createElement('div');
  box.className = 'boot-fail';
  box.innerHTML = `
    <div class="bf-card">
      <b>连不上服务器</b>
      <p>${esc(bootFailReason(err))}</p>
      <button class="btn btn-grad" id="bootRetry">重试</button>
    </div>`;
  document.body.appendChild(box);
  box.querySelector('#bootRetry').addEventListener('click', () => {
    box.remove();
    boot();
  });
}

async function enterApp() {
  $('#login').style.display = 'none';
  $('#app').classList.add('on');
  // 还没有选群：右栏整块收起（三个空壳面板比空着更难看）。
  // 打开会话时 chat.js 会把它去掉。
  if (!state.cur) document.body.classList.add('no-conv');
  try {
    state.scenarios = await api('/v1/scenarios');
  } catch (e) {
    state.scenarios = [];
  }
  await loadLibrary();
  await loadConvs();
  renderDemoBadge();
  renderOnboard();
  const qs2 = new URLSearchParams(location.search);
  if (qs2.get('scenes') || qs2.get('forge')) openScenes();
  if (qs2.get('forge')) {
    $('#forge').classList.add('on');
    renderForge();
    // 默认停在「从角色库找」，先把角色铺出来——
    // 否则用户看到的是一个空框，不知道里面有多少人。
    if (!$('#charList').dataset.loaded) searchChars('');
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
  // 一轮进行中按钮是「停止」，保持可点；只在空闲时按内容切换可用态
  if (!state.busy) $('#sendBtn').disabled = !ta.value.trim();
});
ta.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    if (!state.busy && ta.value.trim()) send();
  }
});
// 发送键在「发送 / 停止」间切换：busy 时点它就是停掉这一轮
$('#sendBtn').addEventListener('click', () => {
  if (state.busy) stopRound();
  else send();
});

/* 附件：纯客户端读文本文件拼进输入框。
   不上传服务器——这里的价值是「把材料递给角色看」，
   内容进了消息就等于进了上下文，无需真的传文件。 */
const attachInput = document.createElement('input');
attachInput.type = 'file';
attachInput.accept = '.txt,.md,.markdown,.csv,.tsv,.json,.yml,.yaml,.xml,.log,.html,.css,.js,.go,.py,.ts,text/*';
attachInput.style.display = 'none';
document.body.appendChild(attachInput);

$('#attachBtn').addEventListener('click', () => attachInput.click());
attachInput.addEventListener('change', async () => {
  const f = attachInput.files && attachInput.files[0];
  attachInput.value = '';   // 同一个文件连选两次也要触发 change
  if (!f) return;
  // 二进制类型给一句人话，别让用户对着静默失败发呆
  if (/\.(png|jpe?g|gif|webp|bmp|svg|ico|pdf|docx?|xlsx?|pptx?|zip|gz|tar|mp[34]|wav|mov|mp4|avi)$/i.test(f.name)) {
    toast('这类文件读不成文字，暂不支持。把内容复制粘贴进来一样有效');
    return;
  }
  try {
    const text = await f.text();
    if (!text.trim()) return toast('这个文件是空的');
    const insert = `📎 附件《${f.name}》：\n${text.trim()}\n`;
    // 超长会撞后端 4000 字上限：先提醒，让用户自己删减
    if ([...ta.value, ...insert].length > 4000) {
      toast('加上这个附件超过 4000 字上限了，放不下——挑关键部分粘贴更好');
      return;
    }
    ta.value += (ta.value && !ta.value.endsWith('\n') ? '\n' : '') + insert;
    ta.dispatchEvent(new Event('input', { bubbles: true }));
    ta.focus();
    toast(`已插入《${f.name}》（${text.length} 字）`);
  } catch (e) {
    toast('文件读取失败，换一个试试');
  }
});

/* @：弹出成员选择，点谁就在光标处插入 @谁 */
$('#mentionBtn').addEventListener('click', () => {
  if (!state.agents.length) return toast('这个群还没有角色');
  const btn = $('#mentionBtn');
  const r = btn.getBoundingClientRect();
  const pop = document.createElement('div');
  pop.className = 'at-pop';
  pop.innerHTML = state.agents.map(a => `
    <button data-at="${esc(a.name)}">
      ${makeAv(a.name, a.color, 'av-sm')}
      <span class="ap-n">${esc(a.name)}</span>
      <span class="ap-t">${esc(a.title || '')}</span>
    </button>`).join('');
  document.body.appendChild(pop);
  const W = 240, H = Math.min(pop.scrollHeight || 260, 280);
  pop.style.left = Math.max(8, Math.min(r.left, window.innerWidth - W - 8)) + 'px';
  pop.style.top = Math.max(8, r.top - H - 8) + 'px';
  requestAnimationFrame(() => pop.classList.add('on'));

  const close = () => {
    pop.classList.remove('on');
    setTimeout(() => pop.remove(), 150);
    document.removeEventListener('mousedown', onDoc, true);
    document.removeEventListener('keydown', onKey);
  };
  function onDoc(e) { if (!pop.contains(e.target) && e.target !== btn) close(); }
  document.addEventListener('mousedown', onDoc, true);
  function onKey(e) { if (e.key === 'Escape') close(); }
  document.addEventListener('keydown', onKey);

  pop.querySelectorAll('[data-at]').forEach(b => {
    b.addEventListener('click', () => {
      // 插在光标处；没有光标信息就追加到末尾
      const pos = ta.selectionStart != null ? ta.selectionStart : ta.value.length;
      const name = b.dataset.at;
      ta.value = ta.value.slice(0, pos) + '@' + name + ' ' + ta.value.slice(pos);
      const np = pos + name.length + 2;
      ta.setSelectionRange(np, np);
      ta.dispatchEvent(new Event('input', { bubbles: true }));
      ta.focus();
      close();
    });
  });
});

// 让群里继续聊：不需要用户发言，角色自己找话题。
// 点击就直接开始——多一步「问话题」的弹窗反而拖慢节奏，
// 想指定话题直接在输入框里说一句就行。
$('#nudgeBtn').addEventListener('click', async () => {
  if (state.busy || !state.cur) return;
  const btn = $('#nudgeBtn');
  btn.disabled = true;
  try {
    await kickoff(state.cur.id, '');
  } finally {
    btn.disabled = false;
  }
});

// 清空当前显示：这是破坏性的（消息从屏幕上消失），
// 之前单击就执行、没有确认，图标还是垃圾桶——看起来像删群。
// 加确认，并把文案说清楚：只影响这一屏，服务端记录还在。
$('#clearBtn').addEventListener('click', async () => {
  if (!state.cur) return;
  const yes = await uiConfirm(
    '清空当前显示？',
    '只是把这一屏清干净，服务端的记录还在——重新打开这个群就能看到。',
    { okText: '清空显示', danger: true }
  );
  if (!yes) return;
  $('#msgWrap').innerHTML = '';
  addSysNote('显示已清空（服务端记录保留）');
});

/* ---------------- 演示模式标记 ---------------- */
function renderDemoBadge() {
  const el = $('#demoBadge');
  if (!el) return;
  // state.me 没拿到就补拉一次——徽章不显示的话，
  // 用户会以为演示台词是真模型输出
  if (!state.me) {
    api('/v1/me').then(d => { state.me = d; renderDemoBadge(); }).catch(() => {});
    return;
  }
  const demo = state.me.demo;
  el.style.display = demo ? 'inline-flex' : 'none';
  if (!demo) return;
  el.onclick = () => {
    UI_dialog_info();
  };
}

// 用轻量的自绘弹层，不引入额外依赖
function UI_dialog_info() {
  const mask = document.createElement('div');
  mask.className = 'ui-mask-demo';
  mask.innerHTML = `
    <div class="ui-card-demo">
      <h3>当前是演示模式</h3>
      <p>现在群里的回复是<b>预置的演示台词</b>，不是真实模型输出。界面、流程、数据存储都是真的，只有「谁说了什么」是假的。</p>
      <p>配上 DeepSeek 的 API Key 重启服务就会切换到真实上游，启动日志会打印 <code>mock:false</code>。</p>
      <div class="ui-act"><button id="uiDemoOk">知道了</button></div>
    </div>`;
  document.body.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  // Esc 监听要跟着浮层走：从「知道了」或点遮罩关闭时也要摘掉，
  // 之前只在这里越积越多（每次开浮层漏一个）
  function onKey(e) { if (e.key === 'Escape') close(); }
  const close = () => {
    mask.classList.remove('on');
    setTimeout(() => mask.remove(), 200);
    document.removeEventListener('keydown', onKey);
  };
  document.addEventListener('keydown', onKey);
  mask.addEventListener('click', e => { if (e.target === mask) close(); });
  mask.querySelector('#uiDemoOk').addEventListener('click', close);
}

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
      hint: '场景页 <em>添加角色</em>',
    },
    {
      done: state.scenarios.some(s => s.custom),
      text: '把角色组合存成工作室',
      hint: '会话行右侧 <em>⋯ → 存为工作室</em>',
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
      <div class="lib-empty">还没有存过角色。点下面「添加角色」——从角色库挑，或者自己造一个，存进来就能在这里勾选。</div></div>`;
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
  // 内置场景和自定义工作室分两段。
  // 混在一起的话，用户分不清哪个是自己做的、哪个是平台给的。
  const card = (s, i) => `
    <button class="scene" data-k="${esc(s.key)}" style="--i:${i}">
      <span class="scene-ic" style="background:linear-gradient(140deg,${hexOf(s.accent)},${shift(hexOf(s.accent), -26)})">
        ${icon(s.icon || 'briefcase', 20)}
      </span>
      <span style="min-width:0;flex:1">
        <span class="scene-n">${esc(s.name)}</span>
        <span class="scene-d">${esc(s.tagline || (s.custom ? '自己调的工作室' : ''))}</span>
        <span class="scene-m">${(s.agents || []).length
          ? (s.agents || []).slice(0, 4).map(a => makeAv(a.name, a.color)).join('')
          : '<em class="scene-rand">开局随机挑 3 个</em>'}</span>
      </span>
    </button>`;

  const builtin = state.scenarios.filter(s => !s.custom);
  const mine = state.scenarios.filter(s => s.custom);

  $('#sceneGrid').innerHTML = `
    <div class="scene-sec">官方给的</div>
    <div class="scene-row">${builtin.map(card).join('')}</div>
    ${mine.length ? `
      <div class="scene-sec">我的工作室 <span>${mine.length}</span></div>
      <div class="scene-row">${mine.map(card).join('')}</div>` : ''}`;

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
  onepiece: ['你们现在到哪了？各自说说接下来想做什么', '路飞，你的五档到底是怎么回事？', '索隆，你和鹰眼还差多少？', '罗宾，历史正文和空白一百年有什么关系？', '如果黑胡子现在打过来，你们怎么分工？'],
  companion: ['今天加班到十点，有点累', '最近有点迷茫', '周末干点什么好'],
};

function renderSceneDetail(key) {
  const sc = state.scenarios.find(s => s.key === key);
  const box = $('#sceneDetail');
  if (!sc) { box.innerHTML = ''; return; }
  const chips = (TRY[key] || []).map(t =>
    `<button class="sd-chip" data-try="${esc(t)}">${esc(t)}</button>`).join('');
  // 随机场景没有写死的角色，得说清楚「为什么这里是空的」，
  // 否则用户会以为这个场景坏了
  const agentBlock = sc.random
    ? `<div class="sd-agents sd-agents-rand">
         <div class="sd-rand-ic">${icon('shuffle', 16)}</div>
         <div>
           <div class="sd-rand-t">开群时从角色库随机挑 3 个</div>
           <div class="sd-rand-d">尽量来自不同作品，凑在一起才有话聊。想指定谁，先用下面的「添加角色」挑好再建群。</div>
         </div>
       </div>`
    : `<div class="sd-agents">
      ${(sc.agents || []).map(a => `
        <div class="sd-agent">
          ${makeAv(a.name, a.color)}
          <div>
            <div class="sd-an">${esc(a.name)}</div>
            <div class="sd-at">${esc(a.title)}</div>
          </div>
        </div>`).join('')}
    </div>`;

  box.innerHTML = `
    <div class="sd-head"><b>${esc(sc.name)}</b>${esc(sc.tagline)}</div>
    ${agentBlock}
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
  const title = await uiPrompt('给这个群起个名字', sc.name, {
    okText: '创建', placeholder: '群名',
    hint: '不改也行，直接用场景名。之后在群菜单里还能改。',
  });
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
    toast(humanError(e));
  }
});

/* ---------------- 角色编辑器 ---------------- */
const forged = [];
let polishIndex = -1;

function renderForge() {
  const box = $('#forgeList');
  if (!forged.length) {
    // 这个列表是「已经选进来、准备入群的」。空的时候说什么，
    // 要看用户在哪一页——在角色库里说「填个名字自己造」是答非所问。
    const onSearch = document.querySelector('.ft.on')?.dataset.ft === 'search';
    // 搜索页：不显示。这行字在角色列表正下方，写了会被读成「列表是空的」，
    // 而实际说的是「已选列表是空的」。底部两个按钮是禁用的，已经说明了状态。
    // 造人页：这里放引导，因为表单就在上方，读起来是连贯的。
    box.innerHTML = onSearch ? '' : `<div style="padding:16px;text-align:center;font-size:12px;color:var(--tx-4)">
      还没有角色。填个名字，让它自己长出来。
    </div>`;
  } else {
    box.innerHTML = forged.map((a, i) => `
      <div class="fcard">
        ${makeAv(a.name, a.color)}
        <div style="min-width:0;flex:1">
          <div class="fcard-n">${esc(a.name)}</div>
          <div class="fcard-t">${esc(a.title)}</div>
          ${a.polished && a.source ? `<div class="fcard-s">${esc(a.source)} · 已按原作润色</div>` : ''}
          <div class="fcard-p">${esc(a.prompt || '')}</div>
        </div>
        <button class="fpolish" data-polish="${i}" title="按原作润色">${icon('sparkles', 14)}</button>
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
    box.querySelectorAll('[data-polish]').forEach(b => {
      b.addEventListener('click', e => {
        e.stopPropagation();
        const i = +b.dataset.polish;
        const a = forged[i];
        if (!a) return;
        polishIndex = i;
        switchForgeTab('polish');
        $('#pName').value = a.name;
        $('#pSource').value = '';
        $('#pNote').value = '';
        showPolishNote('补上作品/来源，检索会更准。', 'info');
        $('#pSource').focus();
      });
    });
  }
  // 一个都没有时计数栏留空——空状态区已经写了「还没有角色。填个名字，
  // 让它自己长出来」，底部再说一遍是重复。
  $('#forgeCount').textContent = forged.length ? forged.length + ' 个角色' : '';
  $('#forgeDone').disabled = forged.length < 1;
  const sv = $('#forgeSave');
  if (sv) sv.disabled = forged.length < 1;
}

$('#openForge').addEventListener('click', () => {
  $('#forge').classList.add('on');
  renderForge();
  if (!$('#charList').dataset.loaded) searchChars('');
});

/* ---------------- 角色库搜索 ---------------- */
// 用户要的是「搜索之后丰富设定」，所以这里先搜再补全，
// 不是让 AI 凭空编一个人。
function switchForgeTab(key) {
  $$('.forge-tabs .ft').forEach(x => x.classList.toggle('on', x.dataset.ft === key));
  $('#paneSearch').style.display = key === 'search' ? '' : 'none';
  $('#paneMake').style.display = key === 'make' ? '' : 'none';
  $('#panePolish').style.display = key === 'polish' ? '' : 'none';
  if (key === 'search' && !$('#charList').dataset.loaded) searchChars('');
  if (key !== 'polish') polishIndex = -1;
}

$$('.forge-tabs .ft').forEach(b => {
  b.addEventListener('click', () => {
    $$('.forge-tabs .ft').forEach(x => x.classList.remove('on'));
    b.classList.add('on');
    const isSearch = b.dataset.ft === 'search';
    $('#paneSearch').style.display = isSearch ? '' : 'none';
    $('#paneMake').style.display = isSearch ? 'none' : '';
    $('#panePolish').style.display = isSearch ? 'none' : '';
    renderForge();  // 空文案跟着 tab 变
    if (isSearch && !$('#charList').dataset.loaded) searchChars('');
  });
});

let charTimer = null;
$('#fSearch').addEventListener('input', e => {
  clearTimeout(charTimer);
  const q = e.target.value.trim();
  charTimer = setTimeout(() => searchChars(q), 220);
});

async function searchChars(q) {
  const box = $('#charList');
  if (!box) return;
  box.innerHTML = '<div class="char-empty">搜索中…</div>';
  try {
    const list = await api('/v1/characters?q=' + encodeURIComponent(q));
    box.dataset.loaded = '1';
    const cnt = document.querySelector('#charCount');
    if (cnt) cnt.textContent = q ? `找到 ${list.length} 个` : `共 ${list.length} 个`;
    if (!list.length) {
      box.innerHTML = '<div class="char-empty">没找到。换个名字或性格试试，比如「毒舌」「傲娇」。</div>';
      return;
    }
    box.innerHTML = list.map(c => `
      <button class="char-item" data-char="${esc(c.name)}">
        ${makeAv(c.name, '#7C3AED')}
        <span>
          <span class="ci-n">${esc(c.name)}</span>
          <span class="ci-w">${esc(c.work)}</span>
        </span>
        <span class="ci-t">${(c.tags || []).slice(0, 2).map(t => `<span>${esc(t)}</span>`).join('')}</span>
      </button>`).join('');

    box.querySelectorAll('[data-char]').forEach(b => {
      b.addEventListener('click', async () => {
        const name = b.dataset.char;
        if (forged.some(a => a.name === name)) { toast('这个角色已经在列表里了'); return; }
        b.disabled = true;
        try {
          // 用角色自带的标签生成完整人设
          const a = await api('/v1/characters/to-agent', { method: 'POST', body: { name } });
          forged.push(a);
          renderForge();
          toast(name + ' 已加入，可以继续找下一个');
        } catch (e) {
          toast(humanError(e));
          b.disabled = false;
        }
      });
    });
  } catch (e) {
    box.innerHTML = '<div class="char-empty">' + esc(e.message) + '</div>';
  }
}
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
    toast(humanError(e));
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

function showPolishNote(msg, kind = 'info') {
  const el = $('#polishNote');
  if (!el) return;
  el.textContent = msg;
  el.className = 'polish-note on ' + kind;
}

function showPolishSources(sources) {
  const el = $('#polishSources');
  if (!el) return;
  const list = (sources || []).filter(s => s && /^https?:\/\//i.test(s.url || ''));
  if (!list.length) {
    el.innerHTML = '';
    el.classList.remove('on');
    return;
  }
  el.innerHTML = '<b>联网来源</b>' + list.slice(0, 5).map(s =>
    `<a href="${esc(s.url)}" target="_blank" rel="noreferrer" title="${esc(s.title || s.url)}">${esc(s.siteName || s.title || '来源')}</a>`
  ).join('');
  el.classList.add('on');
}

let polishing = false;
$('#fPolish').addEventListener('click', async () => {
  if (polishing) return;
  const name = $('#pName').value.trim();
  if (!name) {
    showPolishNote('先填名字，再检索。', 'err');
    $('#pName').focus();
    return;
  }
  const source = $('#pSource').value.trim();
  const kind = $('#pKind').value;
  const note = $('#pNote').value.trim();
  const btn = $('#fPolish');

  polishing = true;
  btn.disabled = true;
  btn.textContent = '检索中…';
  showPolishNote('正在联网检索并写成能直接进群的人设…', 'info');
  showPolishSources([]);
  try {
    const res = await api('/v1/agents/polish', {
      method: 'POST',
      body: { name, source, kind, note },
    });
    if (!res.found || !res.agent || !res.agent.prompt) {
      showPolishSources(res.sources);
      showPolishNote(res.message || '没有找到足够可靠的资料。补充作品/来源，或者换用全名再试。', 'err');
      return;
    }
    const a = res.agent;
    a.polished = true;
    if (polishIndex >= 0 && forged[polishIndex]) {
      forged[polishIndex] = a;
    } else {
      const same = forged.findIndex(x => x.name === a.name);
      if (same >= 0) forged[same] = a;
      else forged.push(a);
    }
    renderForge();
    const from = a.source ? `《${a.source}》` : '原作';
    const conf = a.confidence === 'medium' ? '（置信度中等，建议核对）' : '';
    const web = res.searchUsed ? ` · 联网检索 ${ (res.sources || []).length } 个来源` : '';
    showPolishSources(res.sources);
    showPolishNote(`已润色：${a.name} · ${a.title}，来源 ${from}${web}${conf}`, 'ok');
    toast(a.name + ' 已润色');
    $('#pName').value = '';
    $('#pSource').value = '';
    $('#pNote').value = '';
    polishIndex = -1;
  } catch (e) {
    showPolishNote('检索失败：' + e.message, 'err');
  } finally {
    polishing = false;
    btn.disabled = false;
    btn.textContent = '检索并润色';
  }
});

['#pName', '#pSource', '#pNote'].forEach(sel => {
  $(sel).addEventListener('keydown', e => {
    if (e.key === 'Enter') $('#fPolish').click();
  });
});

// 存入角色库：之后建群可以直接勾选，不用再造一遍
$('#forgeSave').addEventListener('click', async () => {
  if (!forged.length) return;
  const btn = $('#forgeSave');
  btn.disabled = true;
  btn.textContent = '存入中…';
  let saved = 0;
  for (const a of forged) {
    try {
      await api('/v1/agents', {
        method: 'POST',
        body: {
          name: a.name, title: a.title, prompt: a.prompt,
          emoji: a.emoji, color: a.color, source: a.polished ? 'polished' : 'draft',
        },
      });
      saved++;
    } catch (e) { /* 单个失败不影响其他 */ }
  }
  await loadLibrary();
  renderLibPick();
  renderOnboard();
  btn.disabled = false;
  btn.textContent = '存入角色库';
  toast(saved ? `已存入 ${saved} 个，下次建群能直接勾选` : '存入失败');
  if (saved) $('#forge').classList.remove('on');
});

$('#forgeDone').addEventListener('click', async () => {
  if (!forged.length) return;
  const title = await uiPrompt('给这个群起个名字', forged.map(a => a.name).join('、'), {
    okText: '创建', placeholder: '群名',
    hint: '不改也行，用的是角色名。之后还能改。',
  });
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
    toast(humanError(e));
  }
});

/* ---------------- 其他 ---------------- */
$('#logoutBtn').addEventListener('click', () => {
  localStorage.removeItem('ens_token');
  location.reload();
});
/* ---------------- 搜索 ---------------- */
// 跨会话搜消息内容，点结果直接跳到那个群。
let spHits = [];
let spSel = -1;
let spTimer = null;

function openSearch() {
  $('#searchPanel').classList.add('on');
  const inp = $('#spInput');
  inp.value = '';
  inp.focus();
  spHits = [];
  spSel = -1;
  $('#spBody').innerHTML = '<div class="sp-empty">输入关键词，跨所有群搜消息</div>';
}

function closeSearch() {
  $('#searchPanel').classList.remove('on');
}

function renderHits(q) {
  const box = $('#spBody');
  if (!spHits.length) {
    box.innerHTML = `<div class="sp-empty">没找到「${esc(q)}」。换个词试试。</div>`;
    return;
  }
  // 把命中的词标出来（先转义再插 mark，避免 XSS）
  const mark = (text) => {
    const safe = esc(text);
    const idx = safe.toLowerCase().indexOf(esc(q).toLowerCase());
    if (idx < 0) return safe;
    const ql = esc(q).length;
    return safe.slice(0, idx) + '<mark>' + safe.slice(idx, idx + ql) + '</mark>' + safe.slice(idx + ql);
  };
  box.innerHTML = spHits.map((h, i) => `
    <button class="sp-hit ${i === spSel ? 'sel' : ''}" data-hit="${i}">
      <span class="h-top">
        <span class="h-conv">${esc(h.convTitle)}</span>
        <span>·</span>
        <span>${h.role === 'conv' ? '群' : esc(h.agent || '你')}</span>
        <span>·</span>
        <span>${esc(hitTime(h.createdAt))}</span>
      </span>
      <span class="h-txt">${mark(h.snippet)}</span>
    </button>`).join('');
  box.querySelectorAll('[data-hit]').forEach(b => {
    b.addEventListener('click', () => jumpToHit(Number(b.dataset.hit)));
  });
}

// 搜索结果的时间是后端格式化好的「2026-10-22 14:30」。
// 和消息区统一成「今天 14:30 / 昨天 09:12 / 10月18日 20:05」，
// 两边对不上会让人怀疑是不是两套数据。
function hitTime(s) {
  const m = /(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(String(s || ''));
  if (!m) return s || '';
  const d = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]);
  return fullTimeStr(d) || s;
}

async function doSearch(q) {
  if (!q.trim()) {
    $('#spBody').innerHTML = '<div class="sp-empty">输入关键词，跨所有群搜消息</div>';
    spHits = [];
    return;
  }
  $('#spBody').innerHTML = '<div class="sp-empty">搜索中…</div>';
  try {
    const d = await api('/v1/search?q=' + encodeURIComponent(q));
    spHits = d.hits || [];
    spSel = spHits.length ? 0 : -1;
    renderHits(q);
  } catch (e) {
    $('#spBody').innerHTML = '<div class="sp-empty">' + esc(e.message) + '</div>';
  }
}

async function jumpToHit(i) {
  const h = spHits[i];
  if (!h) return;
  closeSearch();
  await openConv(h.convId);
  // 滚到那条消息（历史分页可能还没加载到，找不到就算了）
  setTimeout(() => {
    let el = document.querySelector(`[data-mid="${h.msgId}"]`);
    // 兜底：自己刚发的消息是乐观渲染，data-mid 是临时 id，和服务端的对不上。
    // 用摘要文本匹配一次——摘要就是消息内容的子串。
    if (!el && h.snippet) {
      const key = h.snippet.replace(/^…+|…+$/g, '').slice(0, 16);
      if (key) {
        el = [...document.querySelectorAll('#msgWrap .msg')]
          .find(m => (m._raw || '').includes(key));
      }
    }
    if (el) {
      el.scrollIntoView({ block: 'center', behavior: 'smooth' });
      el.classList.add('flash');
      setTimeout(() => el.classList.remove('flash'), 1400);
    }
  }, 400);
}

$('#searchBtn').addEventListener('click', openSearch);
// 移动端顶栏的搜索图标：820px 以下文字搜索框被收起，没有这个入口就再也搜不了
const searchBtnM = $('#searchBtnM');
if (searchBtnM) searchBtnM.addEventListener('click', openSearch);
// 顶栏 ⌘K 标签跟着平台走（Windows 上写 ⌘ 是误导）
{
  const kbd = $('#searchBtn kbd');
  if (kbd && !/Mac|iPhone|iPad|iPod/.test(navigator.platform || '')) kbd.textContent = 'Ctrl K';
}
$('#spClose').addEventListener('click', closeSearch);
$('#searchPanel').addEventListener('click', e => {
  if (e.target.id === 'searchPanel') closeSearch();
});
$('#spInput').addEventListener('input', e => {
  clearTimeout(spTimer);
  const q = e.target.value;
  spTimer = setTimeout(() => doSearch(q), 220);
});
$('#spInput').addEventListener('keydown', e => {
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    spSel = Math.min(spHits.length - 1, spSel + 1);
    renderHits($('#spInput').value);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    spSel = Math.max(0, spSel - 1);
    renderHits($('#spInput').value);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    jumpToHit(spSel);
  } else if (e.key === 'Escape') {
    e.preventDefault();
    closeSearch();
  }
});

/* ---------------- 键盘导航 ---------------- */
// 专业用户默认用键盘。这里补齐列表导航和常用动作，
// 但任何时候都不抢输入框里的按键。

const inField = el => {
  if (!el) return false;
  const t = el.tagName;
  return t === 'INPUT' || t === 'TEXTAREA' || el.isContentEditable;
};

let kbIndex = -1;   // 会话列表里的当前选中项

function convEls() {
  return [...document.querySelectorAll('#convList .conv')];
}

function focusConv(i) {
  const els = convEls();
  if (!els.length) return;
  kbIndex = Math.max(0, Math.min(els.length - 1, i));
  els.forEach((el, k) => el.classList.toggle('kb-on', k === kbIndex));
  els[kbIndex].scrollIntoView({ block: 'nearest' });
}

function clearKbFocus() {
  kbIndex = -1;
  convEls().forEach(el => el.classList.remove('kb-on'));
}

// 有没有浮层开着（场景页 / 角色编辑器 / 抽屉 / 弹层 / 搜索）
function overlayOpen() {
  return ['#scenes', '#forge', '#drawer', '#ctxMenu', '#searchPanel', '.ui-mask-demo']
    .some(sel => {
      const el = $(sel);
      if (!el) return false;
      return el.classList.contains('on') || el.classList.contains('show');
    });
}

function closeTopOverlay() {
  const order = ['#searchPanel', '.ui-mask-demo', '#ctxMenu', '#forge', '#drawer', '#scenes'];
  for (const sel of order) {
    const el = $(sel);
    if (!el) continue;
    const on = el.classList.contains('on') || el.classList.contains('show');
    if (!on) continue;
    if (sel === '#scenes') { $('#scenes').classList.remove('on'); return true; }
    if (sel === '#forge') { $('#forge').classList.remove('on'); return true; }
    if (sel === '#drawer') { closeDrawer(); return true; }
    if (sel === '#ctxMenu') { closeCtxMenu(); return true; }
    if (sel === '#searchPanel') { closeSearch(); return true; }
    if (sel === '.ui-mask-demo') { el.remove(); return true; }
  }
  return false;
}

document.addEventListener('keydown', e => {
  const mod = e.metaKey || e.ctrlKey;
  const typing = inField(document.activeElement);

  // ⌘K 搜索
  if (mod && (e.key === 'k' || e.key === 'K')) {
    e.preventDefault();
    openSearch();
    return;
  }

  // ⌘N 新建群
  if (mod && (e.key === 'n' || e.key === 'N')) {
    e.preventDefault();
    openScenes();
    return;
  }

  // Esc：逐层关闭。什么都不开时把焦点还给输入框
  if (e.key === 'Escape') {
    if (closeTopOverlay()) return;
    clearKbFocus();
    if (state.cur) { $('#ta').focus(); }
    return;
  }

  // 以下只在没有浮层、且不在输入框里时生效
  if (overlayOpen() || typing) return;

  // ? 打开快捷键说明
  if (e.key === '?') {
    e.preventDefault();
    showShortcuts();
    return;
  }

  // ↑↓ 在会话列表里移动
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault();
    const els = convEls();
    if (!els.length) return;
    if (kbIndex < 0) {
      focusConv(e.key === 'ArrowDown' ? 0 : els.length - 1);
    } else {
      focusConv(kbIndex + (e.key === 'ArrowDown' ? 1 : -1));
    }
    return;
  }

  // Enter 打开选中项
  if (e.key === 'Enter' && kbIndex >= 0) {
    const els = convEls();
    if (els[kbIndex]) {
      e.preventDefault();
      els[kbIndex].click();
      clearKbFocus();
    }
  }
});

function showShortcuts() {
  if ($('.ui-mask-demo')) return;
  // 平台键：Mac 显示 ⌘，其他显示 Ctrl
  const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform || '');
  const MOD = isMac ? '⌘' : 'Ctrl';
  const rows = [
    [MOD + ' K', '搜索'],
    [MOD + ' N', '新建群'],
    ['↑ ↓', '在会话列表里移动'],
    ['Enter', '打开选中的群'],
    ['Esc', '关闭浮层 / 回到输入框'],
    ['Enter', '发送'],
    ['Shift Enter', '换行'],
    ['?', '打开这个列表'],
  ];
  const mask = document.createElement('div');
  mask.className = 'ui-mask-demo';
  mask.innerHTML = `
    <div class="ui-card-demo">
      <h3>键盘快捷键</h3>
      <div class="kb-list">
        ${rows.map(([k, d]) => `<div class="kb-row"><kbd>${k}</kbd><span>${d}</span></div>`).join('')}
      </div>
      <div class="ui-act"><button id="kbOk">知道了</button></div>
    </div>`;
  document.body.appendChild(mask);
  requestAnimationFrame(() => mask.classList.add('on'));
  const close = () => { mask.classList.remove('on'); setTimeout(() => mask.remove(), 200); };
  mask.addEventListener('click', ev => { if (ev.target === mask) close(); });
  mask.querySelector('#kbOk').addEventListener('click', close);
}

// 快捷键入口：新用户不会自己去试 ?，得给个看得见的门
const kbBtn = $('#hintKb');
if (kbBtn) kbBtn.addEventListener('click', showShortcuts);

hydrateIcons();
boot();

// 会话列表点击时打开群的入口（避免 convs.js 直接 import chat.js 成环）
setConvOpener(openConv);

/* ---------------- 断网提示 ---------------- */
// 桌面端该有的基本礼貌：网络没了要说一声，回来了也要说一声。
let wasOffline = false;
function reflectNet(on) {
  const bar = $('#netbar');
  const ta = $('#ta');
  const send = $('#sendBtn');
  if (bar) {
    bar.classList.toggle('on', !on);
    const t = $('#netbarText');
    if (t) {
      t.textContent = on
        ? '连接已恢复'
        : '网络已断开，恢复后会自己接上';
    }
  }
  if (!on) {
    if (ta) ta.disabled = true;
    // 一轮进行中断网也保留「停止」可用：abort 是本地动作，断网时更需要能停
    if (send && !state.busy) send.disabled = true;
    wasOffline = true;
  } else {
    if (ta) {
      ta.disabled = false;
      send.disabled = state.busy ? false : !ta.value.trim();
    }
    if (wasOffline) {
      wasOffline = false;
      toast('连接已恢复');
      // 恢复后先稳住状态：重新拉一次列表，可能错过了别的标签页的改动
      loadConvs().catch(() => {});
    }
  }
}
onNetChange(reflectNet);
reflectNet(isOnline());

/* ---------------- 关页面时的保护 ---------------- */
// 只在「一轮正在说」时拦一下。空闲时拦人很烦——
// 大部分人只是想关个标签页。
//
// 浏览器不允许自定义文案，实际显示的是「确定要离开此网站吗」，
// 所以我们能控制的只有「拦不拦」。
window.addEventListener('beforeunload', e => {
  if (!state.busy) return;
  e.preventDefault();
  e.returnValue = '';   // Chrome 需要这个才弹
  return '';
});

// 供 chat.js 反向刷新引导清单
initMemory();
initStudio();
$('#studioBtn')?.addEventListener('click', openStudio);

window.__refreshOnboard = renderOnboard;
window.__openScenes = openScenes;

// 调试出口：出问题时可以在控制台直接调
window.__ens = { kickoff, send, openConv, loadConvs, state, renderOnboard, openStudio };
