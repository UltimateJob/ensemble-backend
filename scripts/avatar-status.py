#!/usr/bin/env python3
"""打印头像生成进度，并输出仍缺的角色（逗号分隔，供 --only 使用）。"""
import json
import os

have = json.load(open('scripts/avatar-prompts.json', encoding='utf-8'))
have.pop('_comment', None)

char_dir = 'web/app/avatars/characters'
cover_dir = 'web/app/avatars/covers'
done = {f[:-4] for f in os.listdir(char_dir) if f.endswith('.png')} if os.path.isdir(char_dir) else set()
covers = {f[:-4] for f in os.listdir(cover_dir) if f.endswith('.png')} if os.path.isdir(cover_dir) else set()

missing = [k for k in have if k not in done]
print(f'角色 {len(done)}/{len(have)}  封面 {len(covers)}/6  缺 {len(missing)}')
if missing:
    print('MISSING=' + ','.join(missing))
