#!/usr/bin/env python3
"""把文档里的旧产物名 Ensemble.app 换成新的 智团.app。"""
import pathlib

EDITS = {
    'README.md': [
        ('./desktop/build.sh          # 产出 desktop/build/Ensemble.app',
         './desktop/build.sh          # 产出 desktop/build/智团.app'),
        ('open desktop/build/Ensemble.app', 'open desktop/build/智团.app'),
    ],
    'docs/acceptance-work-layer.md': [
        ('open desktop/build/Ensemble.app      # 原生窗口',
         'open desktop/build/智团.app          # 原生窗口'),
    ],
    'docs/team-guide.md': [
        ('open desktop/build/Ensemble.app', 'open desktop/build/智团.app'),
    ],
}

for path, pairs in EDITS.items():
    p = pathlib.Path(path)
    s = p.read_text(encoding='utf-8')
    for old, new in pairs:
        s = s.replace(old, new)
    p.write_text(s, encoding='utf-8')
    print('updated', path)
