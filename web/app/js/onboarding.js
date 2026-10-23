/* ============================================================
   新手引导：四步蒙版
   只在首次登录出现；可跳过，完成状态存 localStorage。
   ============================================================ */

import { esc } from './core.js';

const KEY = 'ens_onboarded';

const STEPS = [
  {
    sel: '#newTeam',
    route: 'lobby',
    title: '先组一个团队',
    text: '点「新建自定义团队」，或者从下面四张卡里直接选一组现成的角色。',
  },
  {
    sel: '#navList .nav-item[data-key="goals"]',
    route: null,
    title: '目标看板',
    text: '把目标挂上角色和任务，进度从真实执行记录派生，不用手填。',
  },
  {
    sel: '#navList .nav-item[data-key="library"]',
    route: null,
    title: '资料库',
    text: '角色产出的标书、代码、文案都会自动归档到这里，随时能翻回去。',
  },
  {
    sel: '#navList .nav-item[data-key="room"]',
    route: null,
    title: '进房间说话',
    text: '在房间顶栏能看到每个角色的状态：灰环空闲、蓝环在干活、绿环刚完成、红环等你审批。',
  },
];

export function maybeStartOnboarding() {
  if (localStorage.getItem(KEY) === '1') return;
  // 引导不应该把用户从当前页面拽走。只有停在大厅时才自动开始；
  // 用户直接进了某个房间，就让他在那里开始用，别打断。
  setTimeout(() => {
    if (!location.hash.startsWith('#/lobby')) return;
    start(0);
  }, 900);
}

function start(i) {
  if (i >= STEPS.length) { done(); return; }
  const s = STEPS[i];

  if (s.route) {
    const want = '#/' + s.route;
    if (s.route === 'room') {
      // 没有房间时跳过这一步，不要卡住引导
      if (!document.querySelector('#avatarRow')) { start(i + 1); return; }
    } else if (!location.hash.startsWith(want)) {
      location.hash = want;
      setTimeout(() => start(i), 600);
      return;
    }
  }

  const target = document.querySelector(s.sel);
  if (!target) { start(i + 1); return; }

  const r = target.getBoundingClientRect();
  if (!r.width || !r.height) { start(i + 1); return; }

  const mask = document.createElement('div');
  mask.className = 'onb-mask';
  const tipLeft = Math.min(Math.max(16, r.left), window.innerWidth - 320);
  const tipTop = Math.min(r.bottom + 14, window.innerHeight - 200);
  mask.innerHTML = `
    <div class="onb-ring" style="left:${r.left - 6}px;top:${r.top - 6}px;width:${r.width + 12}px;height:${r.height + 12}px"></div>
    <div class="onb-tip" style="left:${tipLeft}px;top:${tipTop}px">
      <h4>${esc(s.title)}</h4>
      <p>${esc(s.text)}</p>
      <div class="onb-act">
        <button class="btn btn-line" data-x="skip">跳过</button>
        <button class="btn btn-primary" data-x="next">${i === STEPS.length - 1 ? '完成' : '下一步'}</button>
      </div>
    </div>`;
  document.body.appendChild(mask);
  mask.querySelector('[data-x=skip]').addEventListener('click', () => { mask.remove(); done(); });
  mask.querySelector('[data-x=next]').addEventListener('click', () => {
    mask.remove();
    setTimeout(() => start(i + 1), 120);
  });
}

function done() {
  localStorage.setItem(KEY, '1');
}

/** 设置页「重看引导」用。 */
export function resetOnboarding() {
  localStorage.removeItem(KEY);
}
