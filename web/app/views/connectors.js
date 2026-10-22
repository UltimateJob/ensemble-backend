/* ============================================================
   连接器：团队公共 / 我的私有两组，权限预览与连通性测试
   ============================================================ */

import { api, esc, icon, hydrateIcons, emptyState, toast } from '../js/core.js';
import { openPanel, closePanel, panelOpen } from '../js/shell.js';
import { confirmModal } from '../js/modal.js';
import { stagger } from '../js/motion.js';

// fields 是各家凭据的字段名，顺序即表单顺序。
// configKeys 里的字段走后端 config（非敏感），其余走 secrets。
const KIND = {
  feishu: { label: '飞书文档', ic: 'file-text', fields: ['appId', 'appSecret'] },
  notion: { label: 'Notion', ic: 'book-open', fields: ['token', 'databaseId'] },
  github: { label: 'GitHub', ic: 'code', fields: ['token', 'repo'] },
  calendar: { label: '企业日历', ic: 'calendar', fields: ['url', 'username', 'password'], configKeys: ['url'] },
  webhook: { label: 'Webhook', ic: 'share-2', fields: ['url', 'secret'], configKeys: ['url'] },
};

// 空态里每个服务的一句话用途，比「暂未绑定」有用得多。
const DESC = {
  feishu: '读文档、写纪要',
  notion: '同步知识库页面',
  github: '看代码、提 PR',
  calendar: '查空闲、排会议',
  webhook: '把结果推给你的系统',
};

// 字段名保持原样（要和后端对得上），只在界面上给一个中文说明。
const FIELD_LABEL = {
  appId: 'App ID',
  appSecret: 'App Secret',
  token: '访问令牌',
  databaseId: '数据库 ID',
  repo: '仓库（owner/repo）',
  url: '地址',
  secret: '签名密钥',
  username: '用户名',
  password: '密码',
};

// 每个字段的填写示例。比把字段名再写一遍有意义。
const FIELD_HINT = {
  appId: 'cli_xxxxxxxxxxxx',
  appSecret: '在飞书开放平台 › 应用凭证 里获取',
  token: 'ntn_xxxxxxxxxxxx',
  databaseId: '32 位十六进制 ID',
  repo: 'your-org/your-repo',
  url: 'https://example.com/hook',
  secret: '用于校验请求来源',
  username: '可选',
  password: '可选',
};

let root = null;
let list = [];
let serverKinds = null;

export async function mount(host) {
  root = host;
  await load();
}

export function unmount() {
  root = null;
  list = [];
}

async function load() {
  try {
    const d = await api('/v1/connectors');
    // 后端当前返回 {items, kinds}；兼容只返回数组的情况。
    list = Array.isArray(d) ? d : (d?.items || []);
    const kinds = Array.isArray(d) ? null : d?.kinds;
    if (kinds && Object.keys(kinds).length) serverKinds = kinds;
  } catch (e) {
    list = [];
    toast('连接器加载失败：' + e.message, { type: 'err' });
  }
  render();
}

function kindOf(c) {
  const k = KIND[c.kind];
  if (k) return k;
  // 后端加了新服务类型时，至少还能显示它给的名字。
  return { label: serverKinds?.[c.kind] || c.kind, ic: 'share-2', fields: [] };
}

function statusText(c) {
  if (c.lastError) return c.lastError;
  return c.status === 'ok' ? '连通正常' : '未测试';
}

function row(c) {
  const k = kindOf(c);
  return `<div class="conn-row" data-id="${esc(c.id)}">
    <span class="conn-ic">${icon(k.ic, 18)}</span>
    <div class="conn-body">
      <div class="conn-name">${esc(c.name)} <span class="conn-kind">${esc(k.label)}</span></div>
      <div class="conn-desc" title="${esc(statusText(c))}">${esc(statusText(c))}</div>
    </div>
    <button class="switch ${c.enabled ? 'on' : ''}" data-toggle="${esc(c.id)}"
      title="${c.enabled ? '点击停用' : '点击启用'}" aria-label="${c.enabled ? '停用' : '启用'}"></button>
    <button class="btn btn-line" data-perm="${esc(c.id)}">查看权限</button>
  </div>`;
}

function render() {
  if (!root) return;
  const team = list.filter(c => c.scope === 'team');
  const mine = list.filter(c => c.scope !== 'team');
  root.innerHTML = `
    <div class="page">
      <header class="page-head">
        <div>
          <h1>连接器</h1>
          <p class="page-sub">给 Agent 接上外部服务；凭据只在本地加密保存</p>
        </div>
        <button class="btn btn-primary" id="newConn">${icon('plus', 16)}添加连接器</button>
      </header>
      <section class="sec">
        <h2 class="sec-h">团队公共连接器</h2>
        ${team.length ? team.map(row).join('') : `
          <div class="conn-suggest">
            <div class="cs-lead">接上一个服务，Agent 就能读到真实数据、把结果写回去。</div>
            <div class="cs-grid">
              ${Object.entries(KIND).map(([k, v]) => `
                <button class="cs-card" data-quick="${k}">
                  <span class="cs-ic">${icon(v.ic, 20)}</span>
                  <span class="cs-body">
                    <span class="cs-n">${esc(v.label)}</span>
                    <span class="cs-d">${esc(DESC[k] || '')}</span>
                  </span>
                  <span class="cs-go">${icon('chevron-right', 15)}</span>
                </button>`).join('')}
            </div>
          </div>`}
      </section>
      <section class="sec">
        <h2 class="sec-h">我的私有连接器</h2>
        ${mine.length ? mine.map(row).join('') : `
          <div class="conn-empty-mini">
            ${icon('lock', 15)}
            <span>私有连接器只有你自己能用，团队其他成员看不到。</span>
          </div>`}
      </section>
    </div>`;
  hydrateIcons(root);
  root.querySelectorAll('.sec').forEach(sec => stagger(sec, 30, 200));
  bind();
}

function bind() {
  root.querySelectorAll('[data-toggle]').forEach(b => {
    b.addEventListener('click', async () => {
      const c = list.find(x => x.id === b.dataset.toggle);
      if (!c) return;
      b.disabled = true;
      try {
        await api('/v1/connectors/' + encodeURIComponent(c.id), {
          method: 'PATCH',
          body: { enabled: !c.enabled },
        });
        await load();
      } catch (e) {
        b.disabled = false;
        toast('切换失败：' + e.message, { type: 'err' });
      }
    });
  });

  root.querySelectorAll('[data-perm]').forEach(b => {
    b.addEventListener('click', () => {
      const c = list.find(x => x.id === b.dataset.perm);
      if (c) openPermission(c);
    });
  });

  root.querySelector('#newConn')?.addEventListener('click', addConn);
  root.querySelector('#emptyTeamConn')?.addEventListener('click', addConn);
}

function openPermission(c) {
  const k = kindOf(c);
  openPanel({
    title: c.name,
    subtitle: '权限预览',
    render: el => {
      const keys = c.secretKeys || [];
      el.innerHTML = `
        <div class="panel-sec"><h4>已配置凭据</h4>
          ${keys.length
            ? keys.map(s => `<div class="perm-row"><code>${esc(s)}</code><span class="ok">已填写</span></div>`).join('')
            : '<div class="panel-empty">还没有填写凭据</div>'}
        </div>
        <div class="panel-sec"><h4>该服务需要的字段</h4>
          ${k.fields.length
            ? k.fields.map(f => {
                // url 这类非敏感字段存在 config 里，secretKeys 看不到，得单独判断。
                const filled = keys.includes(f) || !!(c.config && c.config[f]);
                return `<div class="perm-row"><code>${esc(f)}</code>
                  <span class="${filled ? 'ok' : 'dim'}">${filled ? '可用' : '未配置'}</span></div>`;
              }).join('')
            : '<div class="panel-empty">这个服务没有额外字段</div>'}
        </div>
        <div class="panel-sec"><h4>状态</h4>
          <div class="perm-row"><span>最近一次测试</span><span class="${c.status === 'error' ? 'bad' : c.status === 'ok' ? 'ok' : 'dim'}">${esc(statusText(c))}</span></div>
        </div>
        <div class="panel-sec"><h4>操作</h4>
          <div style="display:flex;gap:8px;flex-wrap:wrap">
            <button class="btn btn-line" id="probe">测试连通</button>
            <button class="btn btn-line danger" id="del">删除连接器</button>
          </div>
        </div>`;

      el.querySelector('#probe').addEventListener('click', async () => {
        const btn = el.querySelector('#probe');
        btn.disabled = true;
        btn.textContent = '测试中…';
        try {
          const r = await api('/v1/connectors/' + encodeURIComponent(c.id) + '/probe', { method: 'POST' });
          toast(r.status === 'ok' ? '连通正常' : (r.lastError || '连接失败'), {
            type: r.status === 'ok' ? 'ok' : 'err',
          });
          await load();
          // 面板里的状态文字也要跟着更新；用户已收起面板就别再弹开。
          const fresh = list.find(x => x.id === c.id);
          if (fresh && panelOpen()) openPermission(fresh);
          else { btn.disabled = false; btn.textContent = '测试连通'; }
        } catch (e) {
          toast('测试失败：' + e.message, { type: 'err' });
          btn.disabled = false;
          btn.textContent = '测试连通';
        }
      });

      el.querySelector('#del').addEventListener('click', async () => {
        const yes = await confirmModal({
          title: '删除连接器',
          text: '删除后需要重新填写凭据。',
          okText: '删除',
          danger: true,
        });
        if (!yes) return;
        try {
          await api('/v1/connectors/' + encodeURIComponent(c.id), { method: 'DELETE' });
          closePanel();
          toast('已删除', { type: 'ok' });
          await load();
        } catch (e) {
          toast('删除失败：' + e.message, { type: 'err' });
        }
      });
    },
  });
}

function addConn(presetKind) {
  openPanel({
    title: '添加连接器',
    subtitle: '凭据加密保存，不会明文返回',
    render: el => {
      el.innerHTML = `
        <label class="field"><span>服务</span>
          <select id="cKind">
            ${Object.entries(KIND).map(([k, v]) =>
              `<option value="${esc(k)}">${esc(v.label)}</option>`).join('')}
          </select>
        </label>
        <label class="field"><span>名称</span>
          <input id="cName" maxlength="40" placeholder="比如：团队飞书">
        </label>
        <label class="field"><span>作用域</span>
          <select id="cScope">
            <option value="private">我的私有</option>
            <option value="team">团队公共</option>
          </select>
        </label>
        <div id="cFields"></div>
        <button class="btn btn-primary" id="cSave" style="width:100%">保存</button>`;

      const fieldsBox = el.querySelector('#cFields');
      const drawFields = () => {
        const k = KIND[el.querySelector('#cKind').value] || { fields: [] };
        fieldsBox.innerHTML = k.fields.map(f => `
          <label class="field"><span>${esc(FIELD_LABEL[f] || f)}</span>
            <input data-secret="${esc(f)}" placeholder="${esc(FIELD_HINT[f] || '')}" autocomplete="off">
          </label>`).join('');
      };
      el.querySelector('#cKind').addEventListener('change', drawFields);
      drawFields();

      const save = el.querySelector('#cSave');
      save.addEventListener('click', async () => {
        const kind = el.querySelector('#cKind').value;
        const cfgKeys = KIND[kind]?.configKeys || [];
        const secrets = {};
        const config = {};
        el.querySelectorAll('[data-secret]').forEach(i => {
          const v = i.value.trim();
          if (!v) return;
          // 后端 probe 从 config 读 url，填进 secrets 永远测不通。
          if (cfgKeys.includes(i.dataset.secret)) config[i.dataset.secret] = v;
          else secrets[i.dataset.secret] = v;
        });
        save.disabled = true;
        try {
          await api('/v1/connectors', {
            method: 'POST',
            body: {
              kind,
              name: el.querySelector('#cName').value.trim() || (KIND[kind]?.label || kind),
              scope: el.querySelector('#cScope').value,
              config,
              secrets,
            },
          });
          toast('已保存', { type: 'ok' });
          closePanel();
          await load();
        } catch (e) {
          toast('保存失败：' + e.message, { type: 'err' });
          save.disabled = false;
        }
      });
    },
  });
}
