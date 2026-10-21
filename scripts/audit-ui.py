#!/usr/bin/env python3
"""
静态检查：界面里每个可点的东西背后是不是都有活的逻辑。

    python3 scripts/audit-ui.py

这个脚本是被两个真 bug 逼出来的：
「存入角色库」和「存为工作室」两个按钮，HTML 一直在，
但处理代码在后来的改动中被覆盖了，点了没反应。
接口层测试全绿，用户点不动。
"""
import re, glob, sys, os

os.chdir(os.path.join(os.path.dirname(__file__), '..'))

html = open('web/app/index.html', encoding='utf-8').read()
js = '\n'.join(open(f, encoding='utf-8').read() for f in sorted(glob.glob('web/app/js/*.js')))

problems = []

# 1) 按钮类元素必须有 JS 引用
btn_ids = set(re.findall(r'<button[^>]*id="([a-zA-Z0-9_-]+)"', html))
refs = set(re.findall(r"""["']#([a-zA-Z0-9_-]+)["']""", js))
refs |= set(re.findall(r"getElementById\(['\"]([a-zA-Z0-9_-]+)['\"]\)", js))
dead = sorted(btn_ids - refs)
for d in dead:
    problems.append(f'按钮 #{d} 在 HTML 里，但 JS 里没有任何引用')

# 2) data-act 必须有对应分支
acts = set(re.findall(r'data-act="([a-zA-Z0-9_-]+)"', html))
handled = set(re.findall(r"act === '([a-zA-Z0-9_-]+)'", js))
for a in sorted(acts - handled):
    problems.append(f'菜单项 data-act="{a}" 没有处理分支')

# 3) data-ic 用的图标必须存在
icons = set(re.findall(r'"([a-z0-9-]+)":', open('web/app/icons.js', encoding='utf-8').read()))
used = set(re.findall(r'data-ic="([a-z0-9-]+)"', html))
used |= set(re.findall(r"""icon\('([a-z0-9-]+)'""", js))
for u in sorted(used - icons):
    problems.append(f'图标 "{u}" 被引用但 icons.js 里没有')

# 4) 场景里写的 Icon 字段也必须有对应图标。
#    这是盲区：后端返回的场景 icon 名不进 HTML/JS 的字面量检查，
#    缺了会静默 fallback 成 sparkles——用户看到的是错图标。
go_icons = set()
for f in glob.glob('internal/service/*.go'):
    go_icons |= set(re.findall(r'Icon:\s*"([a-z0-9-]+)"', open(f, encoding='utf-8').read()))
for u in sorted(go_icons - icons):
    problems.append(f'场景用的图标 "{u}" 在 icons.js 里没有（会静默 fallback 成 sparkles）')


# 5) 几条「坏一次就再也看不出来」的 CSS 契约。
#    这些都是实测出来的：出错时页面能渲染、测试能过，只是看着不对。
css = open('web/app/app.css', encoding='utf-8').read()

def rule(selector):
    """取出某个选择器的规则体（粗略但够用）。"""
    m = re.search(re.escape(selector) + r'\s*\{([^}]*)\}', css)
    return m.group(1) if m else None

# 5a 会话列表必须能收缩，否则会话一多就撑破容器压到新手引导卡上
body = rule('.conv-list')
if body is None:
    problems.append('.conv-list 规则不见了')
elif 'min-height:0' not in body.replace(' ', ''):
    problems.append('.conv-list 缺 min-height:0——会话多了会压到引导卡上')

# 5b 产物卡正文要两行截断带省略号，不能靠 JS slice 硬切
body = rule('.artifact .ad')
if body is None:
    problems.append('.artifact .ad 规则不见了')
elif 'line-clamp' not in body:
    problems.append('.artifact .ad 缺 line-clamp——长文案会被硬切，没有省略号')

# 5c 消息区顶部要有渐隐，滚动时最上面那条不该被硬切一半
body = rule('.msgs')
if body is None:
    problems.append('.msgs 规则不见了')
elif 'mask-image' not in body:
    problems.append('.msgs 缺 mask-image——滚动时顶部消息会被硬切')


# 6) 后台也要有移动适配。
#    之前 390 下顶栏导航和卡片标签被逐字换行（「总览」变成两行），
#    因为整页没有一条 max-width 媒体查询。
admin = open('web/admin/index.html', encoding='utf-8').read()
if '@media' not in admin:
    problems.append('web/admin/index.html 没有移动端媒体查询——窄屏下会逐字换行')
else:
    for sel in ['.g4', '.tabs']:
        m = re.search(r'@media[^{]*\{[\s\S]*?' + re.escape(sel) + r'\s*\{', admin)
        if not m:
            problems.append(f'后台媒体查询里没有针对 {sel} 的规则')


# 6b) 后台的卡片式表格靠 data-label 显示字段名。
#     窄屏下 thead 被隐藏，没有 data-label 的 td 就变成没有标签的裸值。
#
#     注意：这些 td 大多是 JS 模板字符串生成的，不在 <table> 标签块里，
#     所以要扫脚本区，不能扫 HTML 区。只排除 colspan（空状态横跨整表）。
script_part = admin[admin.find('<script'):] if '<script' in admin else admin
bare = []
for m in re.finditer(r'<td\b([^>]*)>', script_part):
    attrs = m.group(1)
    if 'colspan' in attrs:
        continue
    if 'data-label' not in attrs:
        bare.append(script_part[max(0, m.start() - 60):m.start()].strip().split('\n')[-1][:40])
if bare:
    problems.append(f'后台有 {len(bare)} 个 td 缺 data-label——窄屏卡片里会是没有字段名的裸值：{bare[:3]}')



# 7) 键盘焦点必须可见。
#    之前只有浏览器默认的 1px 黑框，和紫色强调色不一致，
#    在浅色卡片上也偏弱。checklist C14 要求 2px + offset。
m = re.search(r':focus-visible\s*\{([^}]*)\}', css)
if not m:
    problems.append('app.css 没有 :focus-visible 规则——键盘导航看不到焦点')
else:
    body = m.group(1).replace(' ', '')
    if 'outline' not in body or 'none' in body:
        problems.append(':focus-visible 没有可见 outline')
    elif '2px' not in body:
        problems.append(':focus-visible 的 outline 不是 2px')
    if 'outline-offset' not in body:
        problems.append(':focus-visible 缺 outline-offset')


# 8) 文字对比度。checklist C9：正文 ≥4.5:1，次要文字 ≥3:1。
#    这几个值不是拍脑袋——是量出来不达标之后调深的。
def _rgb(h):
    h = h.lstrip('#')
    return tuple(int(h[i:i+2], 16) for i in (0, 2, 4))

def _lum(rgb):
    def f(c):
        c = c / 255
        return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
    return 0.2126 * f(rgb[0]) + 0.7152 * f(rgb[1]) + 0.0722 * f(rgb[2])

def _ratio(a, b):
    la, lb = _lum(a), _lum(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)

WHITE = (255, 255, 255)
PAGE_BG = _rgb('F3F4F7')
MIN = {'--tx': 7.0, '--tx-2': 4.5, '--tx-3': 4.5, '--tx-4': 3.0, '--pri': 4.5,
       '--ok': 4.5, '--warn': 4.5, '--bad': 4.5}
for tok, need in MIN.items():
    m = re.search(re.escape(tok) + r'\s*:\s*(#[0-9A-Fa-f]{6})', css)
    if not m:
        problems.append(f'找不到 {tok} 的颜色值')
        continue
    c = _rgb(m.group(1))
    worst = min(_ratio(c, WHITE), _ratio(c, PAGE_BG))
    if worst < need:
        problems.append(f'{tok} {m.group(1)} 对比度 {worst:.2f} < {need}（最差背景）')


# 9) 切会话时骨架屏必须铺在第一个 await 之前。
#    放到 await 后面的话，慢网络下这一两秒屏幕上还是上一个群的消息，
#    用户会以为点错了群。这个顺序靠肉眼看代码很容易漏。
chatjs = open('web/app/js/chat.js', encoding='utf-8').read()
m = re.search(r'export async function openConv\(id\)\s*\{(.*?)\n\}', chatjs, re.S)
if not m:
    problems.append('chat.js 里找不到 openConv')
else:
    # 先剥掉注释——注释里提到「await」会误报
    body = re.sub(r'//[^\n]*', '', m.group(1))
    body = re.sub(r'/\*.*?\*/', '', body, flags=re.S)
    i_sk = body.find('showMsgSkeleton()')
    i_aw = body.find('await ')
    if i_sk < 0:
        problems.append('openConv 没有铺骨架屏')
    elif i_aw >= 0 and i_sk > i_aw:
        problems.append('openConv 的骨架屏在第一个 await 之后——慢网络下会先显示上一个群的消息')


# 10) 刷新会话列表失败时，不能把列表清空。
#     网络抖一下侧栏变成「还没有群聊」，用户会以为自己的群全没了。
convsjs = open('web/app/js/convs.js', encoding='utf-8').read()
m = re.search(r'export async function loadConvs\(\)\s*\{(.*?)\n\}', convsjs, re.S)
if not m:
    problems.append('convs.js 里找不到 loadConvs')
else:
    body = re.sub(r'//[^\n]*', '', m.group(1))
    if re.search(r'catch\s*\([^)]*\)\s*\{\s*state\.convs\s*=\s*\[\]', body):
        problems.append('loadConvs 出错时清空了 state.convs——网络抖动会让侧栏变空')

# 11) 发送失败要把原文还给用户，并标记那条消息
chatjs2 = open('web/app/js/chat.js', encoding='utf-8').read()
if 'onFail' not in chatjs2:
    problems.append('send() 没有失败回调——发送失败时原文会丢')
elif 'msg.me.failed' not in open('web/app/app.css', encoding='utf-8').read():
    problems.append('缺少 .msg.me.failed 样式——发送失败的消息看起来像发成功了')

if problems:
    print(f'发现 {len(problems)} 个问题：\n')
    for p in problems:
        print('  ✗ ' + p)
    sys.exit(1)

print(f'可点元素检查通过（{len(btn_ids)} 个按钮 / {len(acts)} 个菜单项 / {len(used)} 个图标）')
