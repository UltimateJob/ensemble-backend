/* ============================================================
   动效：统一的「按下反馈」与「列表错峰入场」。
   只做 CSS 做不了的两件事（拖动、程序化的波纹），其余交给 CSS。
   ============================================================ */

const REDUCED = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/**
 * 给一个容器里的按钮加 iOS 式按压反馈。
 * CSS 的 :active 已经覆盖大部分场景，这里补的是：
 * 触屏上 pointerdown 后手指移出元素时 :active 会卡住不放。
 */
export function pressFeedback(root = document) {
  if (REDUCED) return;
  root.querySelectorAll('.btn, .icon-btn, .team-card, .goal-card, .conn-row, .lib-card')
    .forEach(el => {
      if (el.dataset.pressBound) return;
      el.dataset.pressBound = '1';
      const down = () => el.classList.add('pressing');
      const up = () => el.classList.remove('pressing');
      el.addEventListener('pointerdown', down, { passive: true });
      el.addEventListener('pointerup', up, { passive: true });
      el.addEventListener('pointercancel', up, { passive: true });
      el.addEventListener('pointerleave', up, { passive: true });
    });
}

/** 列表错峰入场：给容器的直接子元素依次加延迟。 */
export function stagger(container, step = 45, max = 320) {
  if (!container || REDUCED) return;
  [...container.children].forEach((el, i) => {
    el.style.animationDelay = Math.min(i * step, max) + 'ms';
    el.classList.add('rise');
  });
}

/**
 * 数字滚动：进度百分比这类会变的数字，直接跳变很生硬。
 * 用 requestAnimationFrame 在 duration 内从 from 走到 to。
 */
export function countTo(el, from, to, duration = 520) {
  if (!el) return;
  if (REDUCED || from === to) { el.textContent = String(to); return; }
  const t0 = performance.now();
  cancelAnimationFrame(el._raf);
  const tick = now => {
    const p = Math.min(1, (now - t0) / duration);
    // easeOutExpo，和 iOS 的减速手感一致
    const e = p === 1 ? 1 : 1 - Math.pow(2, -10 * p);
    el.textContent = String(Math.round(from + (to - from) * e));
    if (p < 1) el._raf = requestAnimationFrame(tick);
  };
  el._raf = requestAnimationFrame(tick);
}

/** 页面切换时的淡出：配合 router 的 mount 动画。 */
export function pageExit(host, done) {
  if (!host || REDUCED) { done(); return; }
  host.style.transition = 'opacity .16s ease, transform .16s ease';
  host.style.opacity = '0';
  host.style.transform = 'translateY(-4px)';
  setTimeout(() => {
    host.style.transition = '';
    host.style.opacity = '';
    host.style.transform = '';
    done();
  }, 150);
}
