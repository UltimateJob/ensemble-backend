/* ============================================================
   轻量 Markdown 渲染。

   为什么不用现成的库：桌面端没有构建步骤，引一个几十 KB 的库要么
   内联进仓库、要么靠 CDN（离线就废）。模型输出的 Markdown 实际只用到
   十来个语法，手写可控且能保证转义安全。

   安全模型：**先整体转义，再套白名单标记**。任何用户/模型内容在进入
   HTML 之前都已经过 esc()，所以不存在注入路径。链接的 href 额外校验
   协议，挡掉 javascript: 这类。

   支持：标题、粗体/斜体/删除线、行内代码、围栏代码块（带语言与复制）、
   有序/无序/任务列表、表格、引用、链接、分隔线。
   ============================================================ */

import { esc } from './core.js';

/** 行内标记。入参必须是已转义的文本。 */
function inline(s) {
  return s
    // 行内代码优先：里面的内容不该再被别的规则改写
    .replace(/`([^`\n]+)`/g, (_, c) => `<code>${c}</code>`)
    // 链接：href 已转义，再校验协议
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, text, href) => {
      const url = safeUrl(href);
      if (!url) return text;
      return `<a href="${url}" target="_blank" rel="noopener noreferrer">${text}</a>`;
    })
    // 裸 URL：不加的话用户只能手动选中复制
    .replace(/(^|[\s(])(https?:\/\/[^\s<)]+)/g, (m, pre, url) =>
      `${pre}<a href="${url}" target="_blank" rel="noopener noreferrer">${url}</a>`)
    .replace(/\*\*([^*\n]+)\*\*/g, '<b>$1</b>')
    .replace(/(?<![*\w])\*([^*\n]+)\*(?!\w)/g, '<i>$1</i>')
    .replace(/~~([^~\n]+)~~/g, '<del>$1</del>');
}

/** 只放行 http(s)、mailto 和站内相对路径。 */
function safeUrl(raw) {
  const u = String(raw).trim();
  if (/^https?:\/\//i.test(u)) return u;
  if (/^mailto:/i.test(u)) return u;
  if (/^\/[^/]/.test(u) || u.startsWith('#')) return u;
  // 其余（javascript:、data:、vbscript: …）一律当普通文本
  return '';
}

/** 把一行按 | 切成单元格，忽略首尾的竖线。 */
function splitRow(line) {
  return line.replace(/^\s*\|/, '').replace(/\|\s*$/, '').split('|').map(c => c.trim());
}

/** 判断一行是不是表格分隔行（| --- | :--: |）。 */
function isTableSep(line) {
  return /^\s*\|?[\s:-]*-[\s|:-]*\|?\s*$/.test(line) && line.includes('-') && line.includes('|');
}

export function mdRender(text) {
  const src = String(text == null ? '' : text);
  const blocks = [];

  // 1. 抽走围栏代码块，内容原样保留
  let s = src.replace(/```([^\n`]*)\n?([\s\S]*?)```/g, (m, lang, code) => {
    const l = String(lang || '').trim();
    const body = code.replace(/\n$/, '');
    blocks.push(
      `<div class="md-code"><div class="md-code-bar"><span>${esc(l || 'code')}</span>` +
      `<button class="md-copy" type="button">复制</button></div>` +
      `<pre><code>${esc(body)}</code></pre></div>`);
    return '\u0000B' + (blocks.length - 1) + '\u0000';
  });

  const out = [];
  const lines = s.split('\n');
  let list = null;
  const closeList = () => { if (list) { out.push('</' + list + '>'); list = null; } };

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];
    const t = raw.trim();

    // 代码块占位
    if (/^\u0000B\d+\u0000$/.test(t)) { closeList(); out.push(t); continue; }
    if (t === '') { closeList(); continue; }

    // 表格：当前行是表头且下一行是分隔行
    if (t.includes('|') && i + 1 < lines.length && isTableSep(lines[i + 1])) {
      closeList();
      const head = splitRow(t);
      i++; // 跳过分隔行
      const rows = [];
      while (i + 1 < lines.length && lines[i + 1].includes('|') && lines[i + 1].trim() !== '') {
        i++;
        rows.push(splitRow(lines[i]));
      }
      out.push('<div class="md-table-wrap"><table class="md-table"><thead><tr>' +
        head.map(h => `<th>${inline(esc(h))}</th>`).join('') +
        '</tr></thead><tbody>' +
        rows.map(r => '<tr>' + r.map(c => `<td>${inline(esc(c))}</td>`).join('') + '</tr>').join('') +
        '</tbody></table></div>');
      continue;
    }

    let m;
    // 标题
    if ((m = /^(#{1,4})\s+(.+)$/.exec(t))) {
      closeList();
      const lv = Math.min(m[1].length + 2, 6); // # → h3（消息里没有 h1 的位置）
      out.push(`<h${lv}>${inline(esc(m[2]))}</h${lv}>`);
      continue;
    }
    // 分隔线
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(t)) { closeList(); out.push('<hr>'); continue; }
    // 引用
    if ((m = /^>\s?(.*)$/.exec(t))) {
      closeList();
      // 连续的引用行合成一块
      const buf = [m[1]];
      while (i + 1 < lines.length && /^>\s?/.test(lines[i + 1].trim())) {
        i++;
        buf.push(lines[i].trim().replace(/^>\s?/, ''));
      }
      out.push(`<blockquote>${inline(esc(buf.join(' ')))}</blockquote>`);
      continue;
    }
    // 任务列表（- [ ] / - [x]）
    if ((m = /^[-*]\s+\[([ xX])\]\s+(.+)$/.exec(t))) {
      if (list !== 'ul') { closeList(); out.push('<ul class="md-task">'); list = 'ul'; }
      const done = m[1].toLowerCase() === 'x';
      out.push(`<li class="${done ? 'done' : ''}"><span class="md-check">${done ? '✓' : ''}</span>${inline(esc(m[2]))}</li>`);
      continue;
    }
    // 无序列表
    if ((m = /^[-*•]\s+(.+)$/.exec(t))) {
      if (list !== 'ul') { closeList(); out.push('<ul>'); list = 'ul'; }
      out.push(`<li>${inline(esc(m[1]))}</li>`);
      continue;
    }
    // 有序列表
    if ((m = /^\d+[.、)]\s+(.+)$/.exec(t))) {
      if (list !== 'ol') { closeList(); out.push('<ol>'); list = 'ol'; }
      out.push(`<li>${inline(esc(m[1]))}</li>`);
      continue;
    }
    // 普通段落
    closeList();
    out.push(`<p>${inline(esc(t))}</p>`);
  }
  closeList();

  return out.join('').replace(/\u0000B(\d+)\u0000/g, (m, i) => blocks[+i]);
}

/**
 * 给渲染结果绑代码块复制。
 * 渲染函数保持纯函数（便于测试），绑定交给调用方。
 */
export function bindCodeCopy(el) {
  el.querySelectorAll('.md-copy').forEach(btn => {
    if (btn.dataset.bound) return;
    btn.dataset.bound = '1';
    btn.addEventListener('click', async e => {
      e.stopPropagation();
      const code = btn.closest('.md-code')?.querySelector('code')?.textContent || '';
      try {
        await navigator.clipboard.writeText(code);
        btn.textContent = '已复制';
        setTimeout(() => { btn.textContent = '复制'; }, 1400);
      } catch {
        btn.textContent = '复制失败';
        setTimeout(() => { btn.textContent = '复制'; }, 1400);
      }
    });
  });
}
