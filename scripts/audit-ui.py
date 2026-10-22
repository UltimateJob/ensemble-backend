#!/usr/bin/env python3
"""
静态检查：界面里每个可点的东西背后是不是都有活的逻辑。

    python3 scripts/audit-ui.py

这个脚本是被两个真 bug 逼出来的：
「存入角色库」和「存为工作室」两个按钮，HTML 一直在，
但处理代码在后来的改动中被覆盖了，点了没反应。
接口层测试全绿，用户点不动。

改版后页面结构变了：index.html 只剩外壳，各页面的 DOM 由
web/app/views/*.js 里的模板字符串生成，样式拆到 web/app/css/*.css。
所以这里的扫描范围也跟着扩到 views 目录和整份 css。
"""
import re, glob, sys, os

os.chdir(os.path.join(os.path.dirname(__file__), '..'))

html = open('web/app/index.html', encoding='utf-8').read()
js_files = sorted(glob.glob('web/app/js/*.js')) + sorted(glob.glob('web/app/views/*.js'))
js = '\n'.join(open(f, encoding='utf-8').read() for f in js_files)
css = '\n'.join(open(f, encoding='utf-8').read() for f in sorted(glob.glob('web/app/css/*.css')))
markup = html + '\n' + js   # 外壳 + 模板字符串生成的 DOM

problems = []

# 1) 按钮类元素必须有 JS 引用。
#    按钮可能写在外壳 HTML 里，也可能由 views/*.js / js/*.js 的模板生成。
btn_ids = set(re.findall(r'<button[^>]*\bid="([a-zA-Z0-9_-]+)"', markup))
refs = set(re.findall(r"""["']#([a-zA-Z0-9_-]+)["']""", js))
refs |= set(re.findall(r"getElementById\(['\"]([a-zA-Z0-9_-]+)['\"]\)", js))
dead = sorted(btn_ids - refs)
for d in dead:
    problems.append(f'按钮 #{d} 在 HTML/模板里，但 JS 里没有任何引用')

# 2) data-act 必须有对应分支。
#    新代码用 addEventListener 绑 [data-act=xxx]，不再走 switch(act)，
#    两种写法都算「有处理」。
acts = set(re.findall(r'data-act="([a-zA-Z0-9_-]+)"', markup))
acts |= set(re.findall(r"data-act=(?:'|\")?([a-zA-Z0-9_-]+)", markup))
handled = set(re.findall(r"act === '([a-zA-Z0-9_-]+)'", js))
handled |= set(re.findall(r"\[data-act=(?:'|\")?([a-zA-Z0-9_-]+)\]", js))
handled |= set(re.findall(r"dataset\.act === '([a-zA-Z0-9_-]+)'", js))
for a in sorted(acts - handled):
    problems.append(f'菜单项 data-act="{a}" 没有处理分支')

# 3) data-ic 用的图标必须存在
icons = set(re.findall(r'"([a-z0-9-]+)":', open('web/app/icons.js', encoding='utf-8').read()))
used = set(re.findall(r'data-ic="([a-z0-9-]+)"', markup))
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
def rule(selector):
    """取出某个选择器的规则体（粗略但够用）。"""
    m = re.search(re.escape(selector) + r'\s*\{([^}]*)\}', css)
    return m.group(1) if m else None

# 5a 消息滚动区顶部要有渐隐，滚动时最上面那条不该被硬切一半。
#    新壳的滚动容器从 .msgs 改成了 .msg-stream。
body = rule('.msg-stream')
if body is None:
    problems.append('.msg-stream 规则不见了')
elif 'mask-image' not in body:
    problems.append('.msg-stream 缺 mask-image——滚动时顶部消息会被硬切')

# 5b 卡片正文要两行截断带省略号，不能靠 JS slice 硬切。
#    产物卡（.artifact .ad）现在单行省略；目标卡描述（.gc-desc）
#    和资料库摘要（.lib-sum）是两行截断。至少得有一处真的 clamp。
body = rule('.gc-desc')
if body is None:
    problems.append('.gc-desc 规则不见了')
elif 'line-clamp' not in body:
    problems.append('.gc-desc 缺 line-clamp——长文案会被硬切，没有省略号')

body = rule('.lib-sum')
if body is None:
    problems.append('.lib-sum 规则不见了')
elif 'line-clamp' not in body:
    problems.append('.lib-sum 缺 line-clamp——长文案会被硬切，没有省略号')


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
# 只匹配独立规则（行首的 :focus-visible），不能匹配
# `.msg-copy:focus-visible{opacity:1}` 这类后代规则。
m = re.search(r'(?:^|\n)\s*:focus-visible\s*\{([^}]*)\}', css)
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
#    改版后页面底色从 #F3F4F7 变成纯白，token 也换了名字。
#    --tx-3/-4 是次要文字（时间、占位、说明），按 3:1 要求；
#    正文类（--tx / --tx-2）按 4.5:1。
#    注意 --pri 是按钮底色，白字压在上面，按 4.5:1 对大号字其实偏严，
#    这里仍按正文标准卡，避免出现「白字看不清」的按钮。
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
# iOS 质感改版后：页面底是暖纸 --paper，卡片是 --sheet 纯白。
# 取两者里更差的做判定。--surface 是旧命名，保留兼容。
SURFACE = _rgb('F4F2EE')   # --paper
MIN = {'--ink': 4.5, '--ink-2': 4.5, '--ink-3': 3.0, '--ink-4': 3.0,
       '--ok-tx': 4.5, '--warn-tx': 4.5, '--bad-tx': 4.5}
for tok, need in MIN.items():
    m = re.search(re.escape(tok) + r'\s*:\s*(#[0-9A-Fa-f]{6})', css)
    if not m:
        problems.append(f'找不到 {tok} 的颜色值')
        continue
    c = _rgb(m.group(1))
    worst = min(_ratio(c, WHITE), _ratio(c, SURFACE))
    if worst < need:
        problems.append(f'{tok} {m.group(1)} 对比度 {worst:.2f} < {need}（最差背景）')

# 白字压在按钮上，真正决定对比度的是按钮底色。
# --pri 是设计稿指定的品牌色（#367BED 对白 4.03），用于描边/图标/链接；
# 承载白字的按钮统一用更深的 --pri-deep。所以这里检查 --pri-deep。
m = re.search(r'--pri-deep\s*:\s*(#[0-9A-Fa-f]{6})', css)
if not m:
    problems.append('找不到 --pri-deep（白字按钮底色）的颜色值')
elif _ratio(_rgb(m.group(1)), WHITE) < 4.5:
    problems.append(f'--pri-deep {m.group(1)} 对白字对比度 {_ratio(_rgb(m.group(1)), WHITE):.2f} < 4.5')


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
#     网络抖一下侧栏变成「还没有房间」，用户会以为自己的群全没了。
convsjs = open('web/app/js/convs.js', encoding='utf-8').read()
m = re.search(r'export async function loadConvs\(\)\s*\{(.*?)\n\}', convsjs, re.S)
if not m:
    problems.append('convs.js 里找不到 loadConvs')
else:
    body = re.sub(r'//[^\n]*', '', m.group(1))
    if re.search(r'catch\s*\([^)]*\)\s*\{\s*state\.convs\s*=\s*\[\]', body):
        problems.append('loadConvs 出错时清空了 state.convs——网络抖动会让侧栏变空')

# 11) 发送失败要把原文还给用户，并标记那条消息
if 'onFail' not in chatjs:
    problems.append('send() 没有失败回调——发送失败时原文会丢')
elif '.msg.me.failed' not in css:
    problems.append('缺少 .msg.me.failed 样式——发送失败的消息看起来像发成功了')

# 12) 错误提示必须经过 humanError 翻译。
#     裸的 toast(e.message) 会把「Failed to fetch」直接甩给用户。
#     新壳里 views/ 下的页面也会弹错，一并检查。
for f in js_files:
    src = open(f, encoding='utf-8').read()
    bare = len(re.findall(r'toast\(\s*[a-z]+\.message\s*\)', src))
    if bare:
        problems.append(f'{f} 有 {bare} 处直接把 e.message 弹出去——要用 humanError')

# 13) 流被截断时必须能看出来。
#     后端每轮末尾发 done；如果流在 done 之前就断了，前端原本
#     和「正常结束」一模一样，用户只会觉得「怎么没人接着说了」。
if "ev.type === 'done'" not in chatjs:
    problems.append('runRound 没有跟踪 done 事件——截断的流会被当成正常结束')
elif 'sawDone' not in chatjs:
    problems.append('runRound 没有 sawDone 标记')
elif '这一轮没说完' not in chatjs:
    problems.append('流被截断时没有给用户任何提示')

# 14) 窄屏必须收起导航标签、且主内容不能整页隐藏。
#     旧壳在 390 宽下写了 .main{display:none}，截图一看整页是空的。
m = re.search(r'@media\s*\(max-width:860px\)\s*\{(.*?)\n\}', css, re.S)
if not m:
    problems.append('shell.css 没有 860px 窄屏断点——移动端没有适配')
else:
    narrow = m.group(1)
    if re.search(r'\.main\s*\{\s*display\s*:\s*none', narrow):
        problems.append('窄屏下 .main 被隐藏——手机上整页会空白')
    if '--nav-w' not in narrow:
        problems.append('窄屏没有收窄导航宽度')

if problems:
    print(f'发现 {len(problems)} 个问题：\n')
    for p in problems:
        print('  ✗ ' + p)
    sys.exit(1)

print(f'可点元素检查通过（{len(btn_ids)} 个按钮 / {len(acts)} 个菜单项 / {len(used)} 个图标）')
