/* ============================================================
   画板：输入描述，生成图片。

   后端 /v1/images 是异步任务（提交返回 id，轮询拿结果）——一张图
   十几秒到一分钟，同步接口会被反代掐断，而上游可能已经出了图。
   所以这里是「提交 → 轮询 → 出图」的形态，不是转圈等。

   没有配生图渠道时，接口返回 503 并说明去哪配，界面上直接照抄。
   ============================================================ */

import { $, api, state, esc, icon, hydrateIcons, toast, humanError } from '../js/core.js';

let root = null;
let pollTimer = null;
let currentJob = null;

export async function mount(host) {
  root = host;
  host.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>画板</h1>
          <p class="page-sub">写清画面，生成图片。生成的图会存进你的工作区</p>
        </div>
      </header>

      <section class="img-compose">
        <label class="field"><span>画面描述</span>
          <textarea id="imgPrompt" rows="4"
            placeholder="比如：一只橘猫坐在窗台上晒太阳，暖色调，胶片质感"></textarea>
        </label>
        <div class="img-opts">
          <label class="field"><span>风格</span>
            <select id="imgStyle">
              <option value="">随机风格（每次可能不同）</option>
            </select>
          </label>
          <label class="field"><span>尺寸</span>
            <select id="imgSize">
              <option value="1024x1024">方形 1024×1024</option>
              <option value="1792x1024">横版 1792×1024</option>
              <option value="1024x1792">竖版 1024×1792</option>
            </select>
          </label>
          <label class="field"><span>张数</span>
            <select id="imgN">
              <option value="1">1 张</option>
              <option value="2">2 张</option>
              <option value="4">4 张</option>
            </select>
          </label>
          <button class="btn btn-primary" id="imgGo">${icon('sparkles', 16)}生成</button>
        </div>
        <div class="hint" id="imgHint"></div>
        <div class="img-style-note hidden" id="imgStyleNote">
          固定风格下每张图用同一个模型，风格一致；随机风格会在多个渠道间轮换，
          单张更省事，但一批图可能风格不一。
        </div>
      </section>

      <section class="sec">
        <h2 class="sec-h">最近生成</h2>
        <div id="imgList" class="img-grid"></div>
      </section>
    </div>`;

  hydrateIcons(host);
  $('#imgGo').addEventListener('click', submit);
  $('#imgStyle').addEventListener('change', () => {
    const fixed = !!$('#imgStyle').value;
    $('#imgStyleNote').classList.toggle('hidden', !fixed);
    updateHint(fixed);
  });
  $('#imgPrompt').addEventListener('keydown', e => {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); submit(); }
  });
  await loadList();
}

export function unmount() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = null;
  currentJob = null;
  root = null;
}

async function submit() {
  const prompt = $('#imgPrompt').value.trim();
  if (!prompt) { toast('先写一句画面描述', { type: 'err' }); return; }
  const btn = $('#imgGo');
  btn.disabled = true;
  try {
    const job = await api('/v1/images', {
      method: 'POST',
      body: {
        prompt,
        size: $('#imgSize').value,
        n: Number($('#imgN').value) || 1,
        provider: $('#imgStyle').value || '',   // 空 = 随机
      },
    });
    currentJob = job;
    renderPending(job);
    startPolling(job.id);
  } catch (e) {
    // 没配渠道时后端返回 503 + 一句可操作的话，直接照抄给用户
    const msg = e.status === 503 ? (e.message || '还没有配置生图渠道') : humanError(e);
    $('#imgHint').textContent = msg;
    toast(msg, { type: 'err' });
    btn.disabled = false;
  }
}

function startPolling(id) {
  if (pollTimer) clearInterval(pollTimer);
  let ticks = 0;
  pollTimer = setInterval(async () => {
    ticks++;
    // 3 分钟还没完就放弃轮询：上游可能卡住了，别一直打接口
    if (ticks > 60) {
      clearInterval(pollTimer);
      pollTimer = null;
      $('#imgHint').textContent = '生成超时了。稍后到「最近生成」里看看，或者重试一次。';
      $('#imgGo').disabled = false;
      return;
    }
    let job;
    try {
      job = await api('/v1/images/' + encodeURIComponent(id));
    } catch (e) {
      return; // 网络抖一下，下一轮再试
    }
    if (job.status === 'done') {
      clearInterval(pollTimer); pollTimer = null;
      $('#imgGo').disabled = false;
      $('#imgHint').textContent = '';
      toast('生成好了', { type: 'ok' });
      await loadList();
    } else if (job.status === 'failed') {
      clearInterval(pollTimer); pollTimer = null;
      $('#imgGo').disabled = false;
      $('#imgHint').textContent = job.error || '生成失败';
      await loadList();
    }
  }, 3000);
}

function renderPending(job) {
  const grid = $('#imgList');
  const el = document.createElement('div');
  el.className = 'img-card pending';
  el.innerHTML = `<div class="img-ph"><span class="spinner"></span></div>
    <div class="img-meta"><span class="ellip">${esc(job.prompt || '')}</span></div>`;
  grid.prepend(el);
  $('#imgHint').textContent = '正在生成，一般十几秒到一分钟…';
}

// updateHint 让提示跟着风格选项走。
// 固定风格时还写「轮流使用」会让人以为设置没生效。
function updateHint(fixed) {
  const el = $('#imgHint');
  if (!el) return;
  const provs = state.__imgProvs || [];
  const names = provs.map(p => p.model || p.name);
  if (fixed) {
    const pick = $('#imgStyle')?.value;
    const hit = provs.find(p => p.name === pick);
    el.textContent = '固定使用 ' + (hit ? (hit.model || hit.name) : pick) + '，每张图风格一致';
    return;
  }
  if (names.length > 1) el.textContent = '随机风格：' + names.join(' / ') + ' 轮流使用';
  else if (names.length === 1) el.textContent = '生图模型：' + names[0];
  else el.textContent = '';
}

async function loadList() {
  const grid = $('#imgList');
  if (!grid) return;
  let data;
  try {
    data = await api('/v1/images');
  } catch (e) {
    grid.innerHTML = `<div class="panel-empty">${esc(humanError(e))}</div>`;
    return;
  }
  // 多渠道路由时告诉用户现在是哪几个模型在轮，
  // 出图风格不一致时这是唯一的线索。
  // providers 现在带模型名：[{name, model}]
  state.__imgProvs = data.providers || [];
  updateHint(!!($('#imgStyle')?.value));

  if (!data.enabled) {
    grid.innerHTML = `<div class="panel-empty">还没有配置生图渠道。到运维台的「上游渠道」里新增一个图片生成类型的渠道。</div>`;
    $('#imgHint').textContent = '生图渠道还没配，现在提交会失败。';
    return;
  }
  // 风格下拉：把可用的渠道填进去。空选项是随机。
  const sel = $('#imgStyle');
  if (sel) {
    const cur = sel.value;
    const provs = data.providers || [];
    sel.innerHTML = '<option value="">随机风格（每次可能不同）</option>' +
      provs.map(p => {
        const label = p.model && p.model !== p.name ? `${p.name} · ${p.model}` : p.name;
        return `<option value="${esc(p.name)}">${esc(label)}</option>`;
      }).join('');
    if (cur) sel.value = cur;
  }

  const items = data.items || [];
  if (!items.length) {
    grid.innerHTML = '<div class="panel-empty">还没有生成过图片</div>';
    return;
  }
  grid.innerHTML = items.map(jobCard).join('');
  grid.querySelectorAll('[data-job]').forEach(b => {
    b.addEventListener('click', () => download(b.dataset.job, b.dataset.idx, b.dataset.name));
  });
  grid.querySelectorAll('[data-thumb]').forEach(async box => {
    try {
      const res = await fetch(`/v1/images/${encodeURIComponent(box.dataset.thumb)}/file?i=0`,
        { headers: { Authorization: 'Bearer ' + (state.token || '') } });
      if (!res.ok) throw new Error('thumb');
      const url = URL.createObjectURL(await res.blob());
      box.innerHTML = `<img src="${url}" alt="" loading="lazy">`;
    } catch {
      box.innerHTML = '<span style="font-size:11px;color:var(--ink-4)">预览不可用</span>';
    }
  });
}

function jobCard(j) {
  if (j.status !== 'done') {
    const label = j.status === 'failed' ? (j.error || '生成失败') : '生成中…';
    return `<div class="img-card pending">
      <div class="img-ph">${j.status === 'failed' ? icon('x', 22) : '<span class="spinner"></span>'}</div>
      <div class="img-meta"><span class="ellip">${esc(j.prompt || '')}</span>
        <span class="img-state${j.status === 'failed' ? ' err' : ''}">${esc(label)}</span></div>
    </div>`;
  }
  const f = (j.files || [])[0];
  if (!f) return '';
  // 直接显示缩略图。图标占位看不出「生成了什么」，等于没生成。
  // 用 job id + 序号取图，带鉴权——img 标签本身发不出 Authorization，
  // 所以先 fetch 成 blob URL。
  return `<div class="img-card">
    <div class="img-ph done" data-thumb="${esc(j.id)}">
      <span class="spinner"></span>
    </div>
    <div class="img-meta">
      <span class="ellip" title="${esc(j.prompt || '')}">${esc(j.prompt || '')}</span>
      <span class="img-state">${esc(fmtSize(f.size))}</span>
    </div>
    <button class="btn btn-line btn-sm img-dl" data-job="${esc(j.id)}" data-idx="0"
      data-name="${esc(basename(f.path))}">${icon('download', 13)}下载</button>
  </div>`;
}

/**
 * 下载要走鉴权：裸 <a href> 不带 Authorization，必然 401。
 *
 * 按 job id + 序号取，而不是让客户端传路径——后者等于给了一个
 * 能读服务器任意文件的接口。服务端只认自己生成过的那些文件。
 */
async function download(jobId, idx, name) {
  try {
    const res = await fetch(`/v1/images/${encodeURIComponent(jobId)}/file?i=${encodeURIComponent(idx)}`,
      { headers: { Authorization: 'Bearer ' + (state.token || '') } });
    if (!res.ok) throw new Error('下载失败（HTTP ' + res.status + '）');
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = name || 'image.png';
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 4000);
  } catch (e) {
    toast(e.message || '下载失败', { type: 'err' });
  }
}

function fmtSize(n) {
  if (!n) return '';
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB';
  return Math.round(n / 1024) + ' KB';
}

function basename(p) {
  return String(p || '').split('/').pop() || 'image.png';
}
