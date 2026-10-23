import { emptyState } from '../js/core.js';

export function mount(host) {
  host.innerHTML = `<div class="page page--center">${emptyState({
    icon: 'search',
    title: '页面不存在',
    sub: '地址可能写错了，或者页面已经被移走',
    action: '<a class="btn btn-primary" href="#/lobby">返回首页</a>',
  })}</div>`;
}

export function unmount() {}
