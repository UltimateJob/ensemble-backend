#!/usr/bin/env node
/**
 * 批量生成黏土头像与团队封面。
 *
 *   IMAGE_API_BASE=https://example.com/v1 IMAGE_API_KEY=... node scripts/avatars.mjs
 *   --dry-run        只打印清单，不发请求
 *   --only=a,b       只生成指定角色（slug 即角色名）
 *   --covers-only    只生成封面
 *
 * 已存在的文件跳过，manifest 由目录实际内容生成。
 * Key 只从环境变量读，不写进仓库。
 */
import { mkdir, writeFile, readFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';

const OUT = 'web/app/avatars';
const BASE = process.env.IMAGE_API_BASE || '';
const KEY = process.env.IMAGE_API_KEY || '';
const MODEL = process.env.IMAGE_MODEL || 'nano-banana-2';
const CONC = Number(process.env.AVATAR_CONC || 3);
const SIZE = process.env.AVATAR_SIZE || '512x512';

const args = process.argv.slice(2);
const dry = args.includes('--dry-run');
const coversOnly = args.includes('--covers-only');
const onlyArg = args.find(a => a.startsWith('--only='));
const only = onlyArg ? onlyArg.slice('--only='.length).split(',') : null;

if (!dry && (!BASE || !KEY)) {
  console.error('缺 IMAGE_API_BASE 或 IMAGE_API_KEY');
  process.exit(2);
}

const CHARACTERS = JSON.parse(await readFile('scripts/avatar-prompts.json', 'utf8'));
delete CHARACTERS._comment;

const COVERS = {
  bid: 'a clay 3d miniature scene of a small team reviewing a long document spread on a table, four simple clay figures, warm muted palette, plain light background, soft studio light, no text',
  xhs: 'a clay 3d miniature scene of a cozy creative desk with a notebook, coffee cup and two small clay figures, soft pink pastel palette, plain light background, no text',
  code: 'a clay 3d miniature scene of a small dev team around an open laptop, blue pastel palette, plain light background, soft studio light, no text',
  anime: 'a clay 3d miniature scene of a cozy tea party with three cheerful clay figures around a small table, soft pastel palette, plain light background, no text',
  onepiece: 'a clay 3d miniature scene of a tiny sailing ship with four cheerful clay figures on deck, warm sunset pastel palette, plain light background, no text',
  research: 'a clay 3d miniature scene of stacked papers and a small chart with tiny clay figures, muted purple pastel palette, plain light background, no text',
  companion: 'a clay 3d miniature scene of a cozy living room with two clay figures chatting on a sofa, warm pastel palette, plain light background, no text',
};

function promptFor(name, desc) {
  return `soft clay 3d portrait of ${desc}, matte clay material, low saturation pastel colors, ` +
    `plain light neutral background, soft studio lighting, head and shoulders, centered, ` +
    `clean minimalist, high detail, no text, no logo, no watermark`;
}

await mkdir(join(OUT, 'characters'), { recursive: true });
await mkdir(join(OUT, 'covers'), { recursive: true });

const jobs = [];
if (!coversOnly) {
  for (const [slug, desc] of Object.entries(CHARACTERS)) {
    if (only && !only.includes(slug)) continue;
    const file = join(OUT, 'characters', slug + '.png');
    if (existsSync(file)) continue;
    jobs.push({ kind: 'characters', slug, prompt: promptFor(slug, desc), file });
  }
}
for (const [slug, p] of Object.entries(COVERS)) {
  if (only && !only.includes('cover-' + slug)) continue;
  const file = join(OUT, 'covers', slug + '.png');
  if (existsSync(file)) continue;
  jobs.push({ kind: 'covers', slug, prompt: p, file });
}

console.log(`待生成 ${jobs.length} 张（已存在跳过），并发 ${CONC}，模型 ${MODEL}`);
if (dry) {
  jobs.forEach(j => console.log(' -', j.kind, j.slug));
  process.exit(0);
}

/**
 * 接口给的是 512×512 原图（约 350KB）。头像最大显示 56px，
 * 256px 足够 4x 屏；封面 480px。用系统 sips 压，不引第三方依赖。
 */
function shrink(file, maxSide) {
  try {
    execFileSync('sips', ['-Z', String(maxSide), '-s', 'formatOptions', '82', file], { stdio: 'ignore' });
  } catch (e) {
    // 没有 sips（非 macOS）就保留原图
  }
}

async function gen(job) {
  for (let attempt = 1; attempt <= 3; attempt++) {
    try {
      const res = await fetch(BASE.replace(/\/$/, '') + '/images/generations', {
        method: 'POST',
        headers: { Authorization: 'Bearer ' + KEY, 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: MODEL, prompt: job.prompt, n: 1, size: SIZE }),
      });
      if (!res.ok) throw new Error('HTTP ' + res.status + ' ' + (await res.text()).slice(0, 160));
      const d = await res.json();
      const b64 = d.data?.[0]?.b64_json;
      if (!b64) throw new Error('响应里没有 b64_json');
      await writeFile(job.file, Buffer.from(b64, 'base64'));
      shrink(job.file, job.kind === 'covers' ? 480 : 256);
      console.log('✓', job.kind, job.slug);
      return true;
    } catch (e) {
      console.warn('×', job.slug, '第', attempt, '次:', e.message);
      await new Promise(r => setTimeout(r, 1500 * attempt));
    }
  }
  return false;
}

let i = 0;
const failed = [];
await Promise.all(Array.from({ length: CONC }, async () => {
  while (i < jobs.length) {
    const job = jobs[i++];
    const ok = await gen(job);
    if (!ok) failed.push(job.slug);
  }
}));

const manifest = { characters: {}, covers: {} };
for (const slug of Object.keys(CHARACTERS)) {
  const rel = 'characters/' + slug + '.png';
  if (existsSync(join(OUT, rel))) manifest.characters[slug] = rel;
}
for (const slug of Object.keys(COVERS)) {
  const rel = 'covers/' + slug + '.png';
  if (existsSync(join(OUT, rel))) manifest.covers[slug] = rel;
}
await writeFile(join(OUT, 'manifest.json'), JSON.stringify(manifest, null, 1) + '\n');

console.log(`完成：角色 ${Object.keys(manifest.characters).length}，封面 ${Object.keys(manifest.covers).length}`);
if (failed.length) {
  console.error('失败：', failed.join(', '));
  process.exit(1);
}
