import { Ionicons } from '@expo/vector-icons';
import React from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { useTheme } from '../theme/theme';
import { gap, leading, radius, type, weight } from '../theme/tokens';
import type { Approval } from '../lib/runs';
import { PulseDot, T, TextButton } from './ui';
import { PressableScale } from './motion';

/**
 * 审批托盘（设计规范 §6.6）。
 *
 * 它**不是**弹窗、也不是浮卡：锚定在输入板上方，与输入框同属一块玻璃容器，
 * 中间用一道发丝线分隔。批准/拒绝后整块塌回成输入板。
 *
 * 结构（自上而下）：
 *   1. 过线小字：琥珀点 + 「需要你的审批」+ 右侧「审批设置」
 *   2. 主体：36px 浅灰圆底 + 盾牌；标题；风险 chip + 该成员级别 + 路径
 *   3. 操作：左「本任务内同类都允许」，右「拒绝」+「允许写入」墨黑胶囊
 *
 * 色彩纪律：除主按钮和琥珀点，整块托盘只有中性灰。
 */
export function ApprovalTray({
  approval,
  level,
  queueCount = 0,
  onAllow,
  onDeny,
  onAllowRun,
  onOpenSettings,
}: {
  approval: Approval;
  /** 申请人当前权限级别，让用户理解「这次为什么要我批」 */
  level?: string;
  /** 排队中的其余条数 */
  queueCount?: number;
  onAllow: () => void;
  onDeny: () => void;
  onAllowRun?: () => void;
  onOpenSettings?: () => void;
}) {
  const t = useTheme();

  const levelLabel =
    level === 'full'
      ? '完全访问'
      : level === 'approval' || level === 'ask'
        ? '需要审批'
        : level === 'readonly'
          ? '只读'
          : '工作区内修改';

  const title = approval.summary || approval.tool || '执行一个操作';
  const path = approval.command || approval.summary || '';
  const riskLabel = RISK[approval.risk] ?? '写入文件';
  const danger = approval.risk === 'delete' || approval.risk === 'privilege';
  const allowLabel = danger ? '允许删除' : '允许写入';

  return (
    <View style={[styles.root, { borderTopColor: t.hairline }]}>
      {/* 1. 过线小字 */}
      <View style={styles.overhead}>
        <View style={styles.overheadLeft}>
          <PulseDot active tone={t.warn} size={6} />
          <T size={type.micro} weight="700" color={t.ink2} tracking={1.2}>
            需要你的审批
          </T>
          {queueCount > 0 ? (
            <T size={type.micro} color={t.ink4} style={{ marginLeft: 2 }}>
              还有 {queueCount} 条
            </T>
          ) : null}
        </View>
        <Pressable onPress={onOpenSettings} hitSlop={10} accessibilityLabel="审批设置">
          <View style={styles.settingsRow}>
            <Ionicons name="options-outline" size={13} color={t.ink4} />
            <T size={type.micro} color={t.ink4} weight="600">
              审批设置
            </T>
          </View>
        </Pressable>
      </View>

      {/* 2. 主体 */}
      <View style={styles.body}>
        <View style={[styles.shield, { backgroundColor: t.chipBg }]}>
          <Ionicons name="shield-checkmark-outline" size={18} color={t.slate} />
        </View>
        <View style={{ flex: 1 }}>
          <T size={type.cardTitle} weight="600" numberOfLines={2}>
            {title}
          </T>
          <View style={styles.metaRow}>
            <View style={[styles.chip, { borderColor: t.hairline }]}>
              <T size={type.micro} color={t.ink3} weight="600">
                {riskLabel}
              </T>
            </View>
            <PulseDot active={false} tone={t.warn} size={5} />
            <T size={type.micro} color={t.ink3} numberOfLines={1}>
              该成员：{levelLabel}
            </T>
          </View>
          {path ? (
            <T
              size={12}
              color={t.ink4}
              numberOfLines={1}
              style={{ marginTop: 5, fontVariant: ['tabular-nums'] }}
            >
              {path}
            </T>
          ) : null}
        </View>
      </View>

      {/* 3. 操作 */}
      <View style={styles.actions}>
        <TextButton label="本任务内同类都允许" onPress={onAllowRun} size={type.caption} fontWeight="500" />
        <View style={styles.actionsRight}>
          <TextButton label="拒绝" onPress={onDeny} size={type.sub} />
          <PressableScale onPress={onAllow} scaleTo={0.97} accessibilityLabel={allowLabel}>
            <View style={[styles.allow, { backgroundColor: t.primaryBg }]}>
              <T size={type.sub} weight="600" color={t.primaryInk}>
                {allowLabel}
              </T>
            </View>
          </PressableScale>
        </View>
      </View>
    </View>
  );
}

const RISK: Record<string, string> = {
  read: '读取',
  write: '写入文件',
  delete: '删除',
  network: '联网',
  exec: '执行命令',
  privilege: '权限',
  critical: '高风险',
};

const styles = StyleSheet.create({
  root: { paddingTop: 12, borderTopWidth: StyleSheet.hairlineWidth },
  overhead: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 18,
  },
  overheadLeft: { flexDirection: 'row', alignItems: 'center', gap: 5 },
  settingsRow: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  body: { flexDirection: 'row', gap: 12, paddingHorizontal: 18, paddingTop: 12 },
  shield: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: 6, marginTop: 6 },
  chip: {
    paddingHorizontal: 7,
    paddingVertical: 2,
    borderRadius: 6,
    borderWidth: StyleSheet.hairlineWidth,
  },
  actions: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 14,
    gap: gap.sm,
  },
  actionsRight: { flexDirection: 'row', alignItems: 'center', gap: gap.md },
  allow: { paddingHorizontal: 20, paddingVertical: 9, borderRadius: radius.pill },
});
