#!/usr/bin/env node
/**
 * 键盘导航测试。
 *
 *   node scripts/keyboard.mjs
 *
 * 专业用户默认用键盘。这里验证列表导航、浮层开关、快捷键说明。
 * 用 CDP 的 Input.dispatchKeyEvent 发真实按键，不是合成 KeyboardEvent。
 */

import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
const PORT=9392,BASE='http://localhost:8080';
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const profile=mkdtempSync(join(tmpdir(),'kb-'));
const proc=spawn('/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',['--headless=new',
    // CI 的 runner 以 root 运行，不加这个 Chrome 起不来
    '--no-sandbox',
    '--disable-dev-shm-usage',`--remote-debugging-port=${PORT}`,'--disable-gpu','--hide-scrollbars',`--user-data-dir=${profile}`,'about:blank'],{stdio:'ignore'});
for(let i=0;i<60;i++){try{if((await fetch(`http://127.0.0.1:${PORT}/json/version`)).ok)break}catch{}await sleep(200)}
const list=await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const ws=new WebSocket(list.find(t=>t.type==='page').webSocketDebuggerUrl);
await new Promise(r=>ws.addEventListener('open',r,{once:true}));
let id=0;const pend=new Map();
ws.addEventListener('message',e=>{const m=JSON.parse(e.data);if(m.id&&pend.has(m.id)){pend.get(m.id)(m.result);pend.delete(m.id)}});
const send=(x,p={})=>new Promise(r=>{const i=++id;pend.set(i,r);ws.send(JSON.stringify({id:i,method:x,params:p}))});
const ev=async e=>{const r=await send('Runtime.evaluate',{expression:e,awaitPromise:true,returnByValue:true});return r.result?.value};
const key=async (k, code, mods=0, text) => {
  await send('Input.dispatchKeyEvent',{type:'keyDown',key:k,code,windowsVirtualKeyCode:code,windowsVirtualKeyCode:code,modifiers:mods,text:text||k});
  await send('Input.dispatchKeyEvent',{type:'keyUp',key:k,code,modifiers:mods});
};
await send('Page.enable');await send('Runtime.enable');
// CI 的 headless 环境里页面可能不是活动的，按键会被丢
await send('Page.bringToFront').catch(()=>{});
const token=(await (await fetch(`${BASE}/v1/auth/login`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({kind:'phone',target:'13800000000'})})).json()).data.token;
// 键盘导航需要至少两个群才能测「下移」。
// 不依赖外部数据，自己准备。
const existing = await fetch(`${BASE}/v1/conversations`, {
  headers: { Authorization: 'Bearer ' + token },
}).then(r => r.json()).then(j => j.data || []);
if (existing.length < 2) {
  for (let i = existing.length; i < 2; i++) {
    await fetch(`${BASE}/v1/conversations`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
      body: JSON.stringify({ title: '键盘测试群 ' + (i + 1), studio: 'companion' }),
    });
  }
}

await send('Page.navigate',{url:`${BASE}/app/?token=${token}`});
await sleep(4000);
let pass=0,fail=0;
const ok=(n,c,d)=>{ if(c){pass++;console.log('  ✓',n)}else{fail++;console.log('  ✗',n,d||'')} };

// ↓ 选中第一个
await send('Input.dispatchKeyEvent',{type:'rawKeyDown',key:'ArrowDown',code:'ArrowDown',windowsVirtualKeyCode:40});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'ArrowDown',code:'ArrowDown',windowsVirtualKeyCode:40});
await sleep(700);
ok('↓ 选中会话', await ev(`document.querySelectorAll('.conv.kb-on').length === 1`));

// 再按一次移动
const first = await ev(`document.querySelector('.conv.kb-on')?.dataset.id`);
await send('Input.dispatchKeyEvent',{type:'rawKeyDown',key:'ArrowDown',code:'ArrowDown',windowsVirtualKeyCode:40});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'ArrowDown',code:'ArrowDown',windowsVirtualKeyCode:40});
await sleep(700);
const second = await ev(`document.querySelector('.conv.kb-on')?.dataset.id`);
ok('再按继续下移', first && second && first !== second, `${first} vs ${second}`);

// Esc 清除
await send('Input.dispatchKeyEvent',{type:'rawKeyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
await sleep(300);
ok('Esc 清除选中', await ev(`document.querySelectorAll('.conv.kb-on').length === 0`));

// ⌘N 打开场景页
await send('Input.dispatchKeyEvent',{type:'rawKeyDown',key:'n',code:'KeyN',windowsVirtualKeyCode:78,modifiers:4});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'n',code:'KeyN',modifiers:4});
await sleep(800);
ok('⌘N 打开场景页', await ev(`document.querySelector('#scenes').classList.contains('on')`));

// Esc 关闭
await send('Input.dispatchKeyEvent',{type:'rawKeyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
await sleep(400);
ok('Esc 关闭场景页', await ev(`!document.querySelector('#scenes').classList.contains('on')`));

// ? 打开快捷键说明
await send('Input.dispatchKeyEvent',{type:'keyDown',key:'?',code:'Slash',windowsVirtualKeyCode:191,modifiers:8,text:'?'});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'?',code:'Slash',modifiers:8});
await sleep(600);
ok('? 打开快捷键说明', await ev(`!!document.querySelector('.ui-mask-demo')`));
const rows = await ev(`document.querySelectorAll('.kb-row').length`);
ok(`说明里有 7 条快捷键（${rows}）`, rows === 7);
await send('Input.dispatchKeyEvent',{type:'rawKeyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
await send('Input.dispatchKeyEvent',{type:'keyUp',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
await sleep(400);
ok('Esc 关闭说明', await ev(`!document.querySelector('.ui-mask-demo')`));

// 「输入框里打 ? 不弹」由代码里的 typing 判断保证。
// headless 下 composer 容器量到 0×0，focus() 不生效，
// 这条没法在这里稳定验证——真实浏览器没问题，E2E 里输入框一直可用。

console.log(`\n${pass} 通过, ${fail} 失败`);
ws.close();proc.kill();
