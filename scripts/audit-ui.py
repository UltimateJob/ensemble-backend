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

if problems:
    print(f'发现 {len(problems)} 个问题：\n')
    for p in problems:
        print('  ✗ ' + p)
    sys.exit(1)

print(f'可点元素检查通过（{len(btn_ids)} 个按钮 / {len(acts)} 个菜单项 / {len(used)} 个图标）')
