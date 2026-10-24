/* ============================================================
   全局搜索：跨所有群搜消息，⌘K 打开。

   后端一直有 /v1/search（跨群搜消息标题与正文），但三栏改版时
   把旧的搜索面板一起删掉了，README 还在宣传这个功能。这里是补回来。

   搜索的意义是「找到那个群、那条结论」，不是导出数据——
   所以结果按群分组，点一条直接跳过去并高亮那条消息。
   ============================================================ */

import { api, esc, icon } from './core.js';
import { openModal, closeModal } from './modal.js';

let inflight = 0;

export function openSearch() {
  const body = `
    <div class="search-box">
      <span class="search-ic">${icon('search', 16)}</span>
      <input id="searchInput" placeholder="搜消息、结论、文件名…" autocomplete="off" spellcheck="false">
      <kbd class="search-esc">Esc</kbd>
    </div>
    <div class="search-results" id="searchResults">
      <div class="search-hint">输入关键词开始搜索。会搜遍你能看到的每个群。</div>
    </div>`;

  openModal({
    title: '搜索',
    width: 640,
    body,
    actions: [],
    onMount: el => {
      const input = el.querySelector('#searchInput');
      const box = el.querySelector('#searchResults');
      input.focus();

      let timer = null;
      input.addEventListener('input', () => {
        clearTimeout(timer);
        const q = input.value.trim();
        if (!q) {
          box.innerHTML = '<div class="search-hint">输入关键词开始搜索。会搜遍你能看到的每个群。</div>';
          return;
        }
        // 250ms 防抖：每敲一个字都发请求会把限流打满
        timer = setTimeout(() => runSearch(q, box), 250);
      });
    },
  });
}

async function runSearch(q, box) {
  const seq = ++inflight;
  box.innerHTML = '<div class="search-hint">搜索中…</div>';
  let data;
  try {
    data = await api('/v1/search?q=' + encodeURIComponent(q) + '&limit=40');
  } catch (e) {
    if (seq !== inflight) return;
    box.innerHTML = `<div class="search-hint err">${esc(e.message || '搜索失败')}</div>`;
    return;
  }
  // 后发的请求已经回来过，丢弃这次结果，避免旧结果覆盖新的
  if (seq !== inflight) return;

  const hits = data?.hits || [];
  if (!hits.length) {
    box.innerHTML = `<div class="search-hint">没有找到「${esc(q)}」。换个说法试试，或者只搜关键词。</div>`;
    return;
  }

  // 按群分组：用户脑子里先想「在哪个群」，再想「哪句话」
  const groups = new Map();
  for (const h of hits) {
    const k = h.convId;
    if (!groups.has(k)) groups.set(k, { title: h.convTitle || '未命名', items: [] });
    groups.get(k).items.push(h);
  }

  box.innerHTML = [...groups.entries()].map(([convId, g]) => `
    <div class="search-group">
      <div class="search-group-h">${esc(g.title)}<em>${g.items.length}</em></div>
      ${g.items.map(h => `
        <button class="search-hit" data-conv="${esc(convId)}" data-msg="${esc(h.msgId || '')}">
          <span class="search-hit-who">${esc(h.agent || h.role || '')}</span>
          <span class="search-hit-body">${highlight(h.snippet || h.content || '', q)}</span>
          <span class="search-hit-time">${esc(fmtTime(h.createdAt))}</span>
        </button>`).join('')}
    </div>`).join('');

  box.querySelectorAll('.search-hit').forEach(b => {
    b.addEventListener('click', () => {
      const convId = b.dataset.conv;
      const msgId = b.dataset.msg;
      closeModal();
      // 带上 msg 参数，进房间后滚动到那条并高亮
      location.hash = '#/room/' + encodeURIComponent(convId) + (msgId ? '?msg=' + encodeURIComponent(msgId) : '');
    });
  });
}

/** 把命中的关键词包成 <mark>，其余内容转义。 */
function highlight(text, q) {
  const safe = esc(text || '');
  const needle = q.trim();
  if (!needle) return safe;
  // 关键词也要转义后再进正则，否则 ( ) 之类会让正则报错
  const pattern = esc(needle).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  try {
    return safe.replace(new RegExp(pattern, 'gi'), m => `<mark>${m}</mark>`);
  } catch {
    return safe;
  }
}

function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d)) return '';
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const p = n => String(n).padStart(2, '0');
  return sameDay ? `${p(d.getHours())}:${p(d.getMinutes())}`
    : `${d.getMonth() + 1}/${d.getDate()}`;
}
