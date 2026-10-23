import { get, post } from './api';
import { getItem, setItem } from './storage';
import type { Conversation } from './types';

/**
 * 默认「纸团」搭档群。
 *
 * 第一次进入 App 时自动建一个，之后一直用它。这是首页对话的来源：
 * 它不是一个"示例群"，而是用户的日常入口——问问题、查资料、解题目都在这。
 *
 * 存在本地的是**群 ID**，不是群本身：群在服务端，换设备/重装后
 * 会重新建一个（旧群仍在团队列表里，不会丢）。
 */
const KEY = 'zhituan_default_conv';

export async function ensureDefaultTeam(): Promise<Conversation | null> {
  // 1. 本地记着的默认群还在吗
  const saved = await getItem(KEY);
  if (saved) {
    try {
      const c = await get<Conversation>(`/v1/conversations/${saved}`);
      if (c?.id) return c;
    } catch {
      // 群被删了，往下走重建
    }
  }

  // 2. 已有会话里找同名/同模板的，避免重复建
  try {
    const list = await get<Conversation[]>('/v1/conversations');
    const found = (list ?? []).find((c) => c.studio === 'companion');
    if (found) {
      await setItem(KEY, found.id);
      return found;
    }
  } catch {
    /* 列表拿不到时继续尝试创建 */
  }

  // 3. 建一个
  try {
    const c = await post<Conversation>('/v1/conversations', {
      title: '纸团',
      studio: 'companion',
    });
    if (c?.id) {
      await setItem(KEY, c.id);
      return c;
    }
  } catch {
    /* 创建失败时返回 null，界面会显示空态 */
  }
  return null;
}

export async function forgetDefaultTeam(): Promise<void> {
  await setItem(KEY, '');
}
