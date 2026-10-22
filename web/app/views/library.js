/* ============================================================
   资料库：团队产出归档的筛选、预览、复制、下载与删除
   ============================================================ */

import {
  api, state, esc, icon, toast, emptyState, skeleton, fullTimeStr,
} from '../js/core.js';
import { openPanel, closePanel } from '../js/shell.js';
import { avatarHTML } from '../js/avatars.js';
import { confirmModal } from '../js/modal.js';
// 复用聊天层的轻量渲染器，避免两处 markdown 行为漂移。
import { mdRender } from '../js/chat.js';

const KINDS = [
  { key: '', label: '全部' },
  { key: 'doc', label: '文档' },
  { key: 'code', label: '代码' },
  { key: 'sheet', label: '表格' },
  { key: 'note', label: '笔记' },
];

let root = null;
let items = [];
let convFilter = '';
let kindFilter = '';
let loading = true;
let loadSeq = 0;   // 快速切筛选时，只认最后一次请求的结果

export function mount(host) {
  root = host;
  convFilter = '';
  kindFilter = '';
  items = [];
  loading = true;
  load();
}

export function unmount() {
  root = null;
}

async function load() {
  // 首次进来才铺骨架；切筛选时保留旧卡片，避免每次选择都闪一下。
  if (!items.length) { loading = true; render(); }
  const seq = ++loadSeq;
  const q = new URLSearchParams({ limit: '60' });
  if (convFilter) {
    q.set('convId', convFilter);
    // 后端目前读的是 conversationId；两个都带，前端不用跟着改。
    q.set('conversationId', convFilter);
  }
  let next = [];
  try {
    next = (await api('/v1/library?' + q.toString())) || [];
  } catch (e) {
    if (seq !== loadSeq) return;
    toast('资料库加载失败：' + e.message, { type: 'err' });
  }
  if (seq !== loadSeq) return;
  items = next;
  loading = false;
  render();
}

function filtered() {
  return kindFilter ? items.filter(a => a.kind === kindFilter) : items;
}

function render() {
  if (!root) return;
  const list = filtered();
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>资料库</h1>
          <p class="page-sub">AI 团队生成的标书、代码、文案会自动归档到这里</p>
        </div>
        <div class="page-actions">
          <select id="libConv" class="select" title="按团队筛选">
            <option value="">全部团队</option>
            ${(state.convs || []).map(c => `
              <option value="${esc(c.id)}"${c.id === convFilter ? ' selected' : ''}>${esc(c.title)}</option>
            `).join('')}
          </select>
        </div>
      </header>

      <div class="seg seg--filter" id="libKind">
        ${KINDS.map(k => `<button data-k="${k.key}" class="${k.key === kindFilter ? 'on' : ''}">${k.label}</button>`).join('')}
      </div>

      ${loading
        ? skeleton('card', 6)
        : list.length
          ? `<div class="lib-grid">${list.map(card).join('')}</div>`
          : emptyState({
              icon: 'book-open',
              title: '暂无团队产出文件',
              sub: 'AI 团队生成的标书、代码、文案会自动归档到这里',
            })}
    </div>`;

  root.querySelectorAll('#libKind button').forEach(b => {
    b.addEventListener('click', () => { kindFilter = b.dataset.k; render(); });
  });
  root.querySelector('#libConv')?.addEventListener('change', e => {
    convFilter = e.target.value;
    load();
  });
  root.querySelectorAll('.lib-card').forEach(el => {
    el.addEventListener('click', () => openArtifact(el.dataset.id));
  });
}

function card(a) {
  const ic = a.kind === 'code' ? 'code' : a.kind === 'sheet' ? 'chart' : 'file-text';
  return `<article class="lib-card" data-id="${esc(a.id)}">
    <div class="lib-thumb">${icon(ic, 28)}</div>
    <div class="lib-body">
      <div class="lib-title">${esc(a.title || '未命名')}</div>
      <div class="lib-sum">${esc(a.summary || '')}</div>
      <div class="lib-meta">
        ${a.agentName ? avatarHTML(a.agentName, '', { size: 20 }) : ''}
        <span>${esc(a.agentName || '')}</span>
        <span class="dim">${esc(fullTimeStr(a.createdAt))}</span>
      </div>
    </div>
  </article>`;
}

async function openArtifact(id) {
  let a;
  try {
    a = await api('/v1/library/' + encodeURIComponent(id));
  } catch (e) {
    toast('打不开：' + e.message, { type: 'err' });
    return;
  }
  const sub = [a.agentName, a.conversationTitle, fullTimeStr(a.createdAt)].filter(Boolean).join(' · ');
  openPanel({
    title: a.title || '产物',
    subtitle: sub,
    render: el => {
      el.innerHTML = `
        <div class="art-body">${renderContent(a)}</div>
        <div class="art-actions">
          <button class="btn btn-line" id="artCopy">复制</button>
          <button class="btn btn-line" id="artDl">下载</button>
          <button class="btn btn-line danger" id="artDel">删除</button>
        </div>`;

      el.querySelector('#artCopy').addEventListener('click', async () => {
        const ok = await copyText(a.content || '');
        toast(ok ? '已复制' : '复制失败，手动选中文本吧', { type: ok ? 'ok' : 'err' });
      });
      el.querySelector('#artDl').addEventListener('click', () => download(a));
      el.querySelector('#artDel').addEventListener('click', async () => {
        if (!await confirmModal({
          title: '删除产物',
          text: `「${a.title || '未命名'}」删除后不能恢复。`,
          okText: '删除',
          danger: true,
        })) return;
        try {
          await api('/v1/library/' + encodeURIComponent(a.id), { method: 'DELETE' });
          closePanel();
          await load();
          toast('已删除', { type: 'ok' });
        } catch (e) {
          toast('删除失败：' + e.message, { type: 'err' });
        }
      });
    },
  });
}

function renderContent(a) {
  const c = a.content || '';
  if (a.kind === 'sheet' || a.kind === 'code') return `<pre class="art-pre">${esc(c)}</pre>`;
  return `<div class="md">${renderMarkdown(c)}</div>`;
}

// chat.js 的 mdRender 只认粗体 / 行内代码 / 围栏 / 列表，标书这类文档的
// 「# 一级标题」会原样留成段落。它的输出已经整体转义过，这里只把
// 「整段就是一个标题」的 <p> 提升成 h1–h3，其余仍交给它渲染。
function renderMarkdown(src) {
  return mdRender(src).replace(/<p>(#{1,3})\s+([\s\S]*?)<\/p>/g,
    (m, hashes, text) => `<h${hashes.length}>${text}</h${hashes.length}>`);
}

async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch (e) { /* 走老式回退 */ }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;opacity:0;pointer-events:none';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  } catch (e) {
    return false;
  }
}

function download(a) {
  const ext = a.kind === 'sheet' ? 'csv' : a.kind === 'code' ? 'txt' : 'md';
  const safe = String(a.title || 'artifact').replace(/[\\/:*?"<>|]/g, '_');
  const blob = new Blob([a.content || ''], { type: 'text/plain;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = safe + '.' + ext;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 3000);
}
