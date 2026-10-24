import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
  useWindowDimensions,
} from 'react-native';
import { Avatar } from './Avatar';
import { Icon, RadioDot, Row, T, Tag, withAlpha } from './ui';
import { PressableScale } from './motion';
import { get, humanError, patch } from '../lib/api';
import type { AgentActivity, Conversation, Run } from '../lib/types';
import { useTheme, type Theme } from '../theme/theme';
import { gap, leading, radius, shadow, type, weight } from '../theme/tokens';

/**
 * 成员详情（设计规范 §9 第 4 屏，图 09）。
 *
 * 形态是 bottom sheet：从底部升起，顶部有拖拽手柄，点遮罩关闭。
 *
 * 内容自上而下：
 *   72 头像（带状态点）+ 名字 + 「角色 · 群名」
 *   → 等审批胶囊（红底红字）
 *   → 当前动态（说明 + 进度 + 任务 n / m 步）
 *   → 人设 / 权限三级单选（§7）/ 使用模型
 *   → 运行记录（最多 4 条）
 *   → 底部：刷新状态（白底描边）+ 单独发指令（墨黑胶囊）
 *
 * 权限单选直接 PATCH 会话的 agentPermissions（成员级覆盖群默认），
 * 先动 UI 再等接口，失败回滚。
 */

const LEVELS = [
  { key: 'ask', label: '需要审批', hint: '执行任何操作前都先问过你' },
  { key: 'approval', label: '工作区内修改', hint: '区内可自由读写；联网、删除、区外仍询问' },
  {
    key: 'full',
    label: '完全访问权限',
    hint: '全部操作不再询问',
    warn: true,
  },
] as const;

export function AgentPanel({
  visible,
  agent,
  activity,
  convId,
  workspaceId,
  onClose,
  onDirect,
  onToggleCapability,
}: {
  visible: boolean;
  agent: {
    key: string;
    name: string;
    title?: string;
    color?: string;
    capabilities?: string[];
  } | null;
  activity?: AgentActivity;
  convId: string;
  workspaceId?: string;
  onClose: () => void;
  /** 「单独发指令」：关掉面板，把 @他 填进输入框，光标落在后面。 */
  onDirect?: (agentName: string) => void;
  /** 切换某项能力（目前只有生图）。不传则不显示这一块。 */
  onToggleCapability?: (cap: string, on: boolean) => Promise<void>;
}) {
  const th = useTheme();
  const s = useMemo(() => mkStyles(th), [th]);
  const { height } = useWindowDimensions();
  const panelH = height * 0.88;
  const slide = useRef(new Animated.Value(1)).current;

  const [run, setRun] = useState<Run | null>(null);
  const [history, setHistory] = useState<Run[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

  const [convTitle, setConvTitle] = useState('');
  const [level, setLevel] = useState(activity?.permission ?? '');
  const [levelErr, setLevelErr] = useState('');
  const [savingLevel, setSavingLevel] = useState(false);

  useEffect(() => {
    Animated.spring(slide, {
      toValue: visible ? 0 : 1,
      useNativeDriver: true,
      damping: 26,
      stiffness: 260,
      mass: 0.8,
    }).start();
  }, [visible, slide]);

  useEffect(() => {
    setLevel(activity?.permission ?? '');
    setLevelErr('');
  }, [activity?.permission, agent?.key]);

  useEffect(() => {
    if (!visible || !agent) return;
    let alive = true;
    (async () => {
      setErr('');
      setLoading(true);
      try {
        if (convId) {
          const c = await get<Conversation>(`/v1/conversations/${convId}`);
          if (alive && c?.title) setConvTitle(c.title);
        }
        if (activity?.runId) {
          const r = await get<Run>(`/v1/runs/${activity.runId}`);
          if (alive) setRun(r);
        } else if (workspaceId) {
          const list = await get<Run[]>(`/v1/workspaces/${workspaceId}/runs`);
          const mine = (list ?? []).filter((r) => r.operatorKey === agent.key);
          if (alive) {
            setRun(mine[0] ?? null);
            setHistory(mine.slice(1, 8));
          }
        } else if (alive) {
          setRun(null);
          setHistory([]);
        }
      } catch (e) {
        if (alive) setErr(humanError(e));
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [visible, agent, activity?.runId, workspaceId, convId, reloadKey]);

  if (!visible || !agent) return null;

  const state = activity?.state;
  const blocked = state === 'blocked';
  const dotColor =
    state === 'blocked'
      ? th.dangerDot
      : state === 'done'
        ? th.okDot
        : state === 'working'
          ? th.ink
          : th.ink4;

  const step = run?.step ?? activity?.step ?? 0;
  const maxSteps = run?.maxSteps ?? activity?.maxSteps ?? 0;
  const pct = maxSteps > 0 ? Math.min(100, (step / maxSteps) * 100) : 0;
  const detail =
    activity?.detail ||
    run?.summary ||
    (run?.task ? `正在处理：${run.task}` : '现在没有在跑的任务，私活不会刷到群聊。');

  const pillText = `等你审批 · ${
    activity?.currentTool?.summary ||
    activity?.detail ||
    run?.task ||
    '等待你确认'
  }`;

  const records = buildRecords(history, run).slice(0, 4);
  const subtitle = [
    agent.title || '团队成员',
    convTitle || null,
  ].filter(Boolean).join(' · ');

  const [capBusy, setCapBusy] = useState<string | null>(null);
  const [capErr, setCapErr] = useState('');

  async function toggleCap(cap: string, on: boolean) {
    if (!onToggleCapability || capBusy) return;
    setCapErr('');
    setCapBusy(cap);
    try {
      await onToggleCapability(cap, on);
    } catch (e) {
      setCapErr(humanError(e));
    } finally {
      setCapBusy(null);
    }
  }

  async function pickLevel(next: string) {
    if (next === level || savingLevel || !convId) return;
    const prev = level;
    setLevel(next);
    setLevelErr('');
    setSavingLevel(true);
    try {
      await patch(`/v1/conversations/${convId}`, {
        agentPermissions: { [agent!.key]: next },
      });
    } catch (e) {
      setLevel(prev);
      setLevelErr(humanError(e));
    } finally {
      setSavingLevel(false);
    }
  }

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none">
      <Pressable style={s.scrim} onPress={onClose} accessibilityLabel="关闭成员详情" />

      <Animated.View
        style={[
          s.panel,
          {
            transform: [
              {
                translateY: slide.interpolate({ inputRange: [0, 1], outputRange: [0, panelH] }),
              },
            ],
          },
        ]}
      >
        {/* 拖拽手柄：iOS bottom sheet 的标准视觉语言 */}
        <View style={s.handleWrap}>
          <View style={s.handle} />
        </View>

        <Pressable
          onPress={onClose}
          hitSlop={10}
          accessibilityRole="button"
          accessibilityLabel="关闭"
          style={s.close}
        >
          <Icon name="close" size={20} color={th.ink3} />
        </Pressable>

        <ScrollView
          contentContainerStyle={s.body}
          showsVerticalScrollIndicator={false}
        >
          {/* 头像 + 名字 + 角色 · 群名 */}
          <View style={s.identity}>
            <View style={{ width: 72, height: 72 }}>
              <Avatar name={agent.name} size={72} tint={agent.color} />
              <View
                style={[
                  s.bigDot,
                  { backgroundColor: dotColor, borderColor: th.card },
                ]}
              />
            </View>
            <T size={type.page} weight={weight.bold} style={{ marginTop: gap.md }}>
              {agent.name}
            </T>
            <T size={type.sub} color={th.ink3} align="center" style={{ marginTop: 4 }}>
              {subtitle}
            </T>

            {/* 状态胶囊：等审批红底红字，置于头像下方（§9 第 4 屏） */}
            {blocked ? (
              <View
                style={[
                  s.statusPill,
                  { backgroundColor: blocked ? th.badSoft : th.cardSunken },
                ]}
              >
                <View
                  style={[
                    s.pillDot,
                    { backgroundColor: blocked ? th.dangerDot : dotColor },
                  ]}
                />
                <T
                  size={type.caption}
                  weight={weight.semibold}
                  color={blocked ? th.bad : th.ink2}
                  numberOfLines={1}
                  style={{ flexShrink: 1 }}
                >
                  {pillText}
                </T>
              </View>
            ) : null}
          </View>

          {loading ? <ActivityIndicator color={th.ink3} style={{ marginTop: gap.md }} /> : null}
          {err ? (
            <T size={type.sub} color={th.bad} style={{ marginTop: gap.sm }}>
              {err}
            </T>
          ) : null}

          {/* 当前动态 */}
          <View style={s.activityCard}>
            <T size={type.caption} weight={weight.strong} color={th.ink3} tracking={0.6}>
              当前动态
            </T>
            <T size={type.body} lh={leading.body} style={{ marginTop: gap.xs }}>
              {detail}
            </T>
            <Row gap={gap.sm} style={{ marginTop: gap.md }}>
              <View
                style={[
                  s.track,
                  { backgroundColor: th.dark ? withAlpha(th.white, 0.14) : th.track },
                ]}
              >
                <View
                  style={[
                    s.fill,
                    {
                      width: `${pct}%`,
                      backgroundColor: th.dark ? '#F2F2F2' : th.ink,
                    },
                  ]}
                />
              </View>
              <T
                size={type.micro}
                weight={weight.semibold}
                color={th.ink2}
                style={{ fontVariant: ['tabular-nums'] }}
              >
                任务 {step} / {maxSteps || '—'} 步
              </T>
            </Row>
          </View>

          {/* 人设 / 权限 / 模型 */}
          <View style={s.card}>
            {/* 人设只读展示：这是角色卡上的设定，不从这里改。
                右侧的箭头以前暗示可以点进去，实际点了没反应，已去掉。 */}
            <View style={s.settingRow}>
              <T size={type.card} weight={weight.strong} style={{ flex: 1 }}>
                人设
              </T>
              <T size={type.caption} color={th.ink3} numberOfLines={1} style={{ maxWidth: '60%' }}>
                {agent?.title || '跟随角色设定'}
              </T>
            </View>

            <View style={[s.hair, { backgroundColor: th.line }]} />

            <View style={{ paddingVertical: gap.xs, gap: 2 }}>
              {LEVELS.map((l) => {
                const on = level === l.key;
                return (
                  <Pressable
                    key={l.key}
                    onPress={() => pickLevel(l.key)}
                    disabled={savingLevel || !convId}
                    accessibilityRole="radio"
                    accessibilityState={{ selected: on, disabled: savingLevel || !convId }}
                    style={({ pressed }) => [
                      s.levelRow,
                      { backgroundColor: on ? th.cardSunken : 'transparent' },
                      pressed && { opacity: 0.7 },
                    ]}
                  >
                    <RadioDot on={on} />
                    <View style={{ flex: 1 }}>
                      <Row gap={6}>
                        <T size={type.card} weight={weight.strong}>
                          {l.label}
                        </T>
                        {'warn' in l && l.warn ? <Tag label="谨慎授予" tone="gold" /> : null}
                      </Row>
                      <T size={type.micro} color={th.ink3} lh={leading.micro} style={{ marginTop: 2 }}>
                        {l.hint}
                      </T>
                    </View>
                  </Pressable>
                );
              })}
              {levelErr ? (
                <T size={type.micro} color={th.bad} style={{ marginTop: 4, paddingHorizontal: 9 }}>
                  {levelErr}
                </T>
              ) : null}
            </View>

            {onToggleCapability ? (
              <>
                <View style={[s.hair, { backgroundColor: th.line }]} />
                <View style={s.settingRow}>
                  <View style={{ flex: 1 }}>
                    <T size={type.card} weight={weight.strong}>
                      生成图片
                    </T>
                    <T size={type.micro} color={th.ink3} style={{ marginTop: 2 }}>
                      授予后它才能在对话里直接出图
                    </T>
                  </View>
                  {capBusy === 'image' ? (
                    <ActivityIndicator size="small" color={th.ink3} />
                  ) : (
                    <Pressable
                      onPress={() =>
                        void toggleCap('image', !(agent?.capabilities ?? []).includes('image'))
                      }
                      accessibilityRole="switch"
                      accessibilityState={{
                        checked: (agent?.capabilities ?? []).includes('image'),
                      }}
                      accessibilityLabel="生成图片能力"
                    >
                      <View
                        style={{
                          width: 51,
                          height: 31,
                          borderRadius: 16,
                          padding: 2.5,
                          justifyContent: 'center',
                          backgroundColor: (agent?.capabilities ?? []).includes('image')
                            ? th.primaryBg
                            : th.cardSunken,
                        }}
                      >
                        <View
                          style={[
                            s.capKnob,
                            {
                              backgroundColor: th.card,
                              alignSelf: (agent?.capabilities ?? []).includes('image')
                                ? 'flex-end'
                                : 'flex-start',
                            },
                          ]}
                        />
                      </View>
                    </Pressable>
                  )}
                </View>
                {capErr ? (
                  <T size={type.micro} color={th.bad} style={{ paddingHorizontal: 9, paddingBottom: 6 }}>
                    {capErr}
                  </T>
                ) : null}
              </>
            ) : null}

            <View style={[s.hair, { backgroundColor: th.line }]} />

            <View style={s.settingRow}>
              <T size={type.card} weight={weight.strong} style={{ flex: 1 }}>
                使用模型
              </T>
              <T size={type.caption} color={th.ink3} numberOfLines={1} style={{ maxWidth: '60%' }}>
                {run?.model || '按任务自动选择'}
              </T>
            </View>
          </View>

          {/* 运行记录：时间 + 一句话，最多 4 条 */}
          <T
            size={type.caption}
            weight={weight.strong}
            color={th.ink3}
            style={{ marginTop: gap.lg, marginBottom: gap.xs }}
          >
            运行记录
          </T>
          {records.length === 0 ? (
            <T size={type.sub} color={th.ink4}>
              还没有运行记录
            </T>
          ) : (
            records.map((r, i) => (
              <View key={r.id}>
                {i > 0 ? <View style={[s.hair, { backgroundColor: th.line }]} /> : null}
                <Row gap={gap.md} align="flex-start" style={{ paddingVertical: 11 }}>
                  <T
                    size={type.caption}
                    color={th.ink4}
                    style={{ width: 44, fontVariant: ['tabular-nums'] }}
                  >
                    {fmtClock(r.startedAt || r.endedAt)}
                  </T>
                  <T size={type.sub} color={th.ink2} lh={leading.sub} style={{ flex: 1 }} numberOfLines={2}>
                    {r.summary || r.task}
                  </T>
                </Row>
              </View>
            ))
          )}
        </ScrollView>

        <View style={s.footer}>
          <PressableScale
            onPress={() => setReloadKey((k) => k + 1)}
            scaleTo={0.97}
            accessibilityLabel="刷新状态"
            style={{ flex: 1 }}
          >
            <View style={[s.btnSecondary, { backgroundColor: th.card, borderColor: th.line }]}>
              <T size={type.card} weight={weight.semibold} color={th.ink}>
                刷新状态
              </T>
            </View>
          </PressableScale>
          <PressableScale
            onPress={() => {
              if (!agent || !onDirect) return onClose();
              onDirect(agent.name);
            }}
            scaleTo={0.97}
            accessibilityLabel="单独发指令"
            style={{ flex: 1 }}
          >
            <View style={[s.btnPrimary, { backgroundColor: th.primaryBg }]}>
              <T size={type.card} weight={weight.semibold} color={th.primaryInk}>
                单独发指令
              </T>
            </View>
          </PressableScale>
        </View>
      </Animated.View>
    </View>
  );
}

interface Rec {
  id: string;
  startedAt: string;
  endedAt?: string;
  summary?: string;
  task: string;
}

/** 当前 run 也进记录；同一条不重复。 */
function buildRecords(history: Run[], run: Run | null): Rec[] {
  const list: Rec[] = [];
  if (run) list.push(run);
  for (const h of history) {
    if (run && h.id === run.id) continue;
    list.push(h);
  }
  return list.sort((a, b) => {
    const at = Date.parse(a.endedAt || a.startedAt || '') || 0;
    const bt = Date.parse(b.endedAt || b.startedAt || '') || 0;
    return bt - at;
  });
}

function fmtClock(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

const mkStyles = (th: Theme) =>
  StyleSheet.create({
    scrim: {
      position: 'absolute',
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      backgroundColor: th.scrim,
    },
    panel: {
      position: 'absolute',
      bottom: 0,
      left: 0,
      right: 0,
      maxHeight: '88%',
      minHeight: 320,
      backgroundColor: th.card,
      borderTopLeftRadius: 28,
      borderTopRightRadius: 28,
      overflow: 'hidden',
      ...shadow.tray,
    },
    handleWrap: { alignItems: 'center', paddingTop: 8, paddingBottom: 2 },
    handle: {
      width: 36,
      height: 4,
      borderRadius: 2,
      backgroundColor: th.dark ? withAlpha(th.white, 0.18) : withAlpha(th.ink, 0.14),
    },
    close: {
      position: 'absolute',
      top: 14,
      right: 14,
      width: 36,
      height: 36,
      borderRadius: 18,
      alignItems: 'center',
      justifyContent: 'center',
      zIndex: 2,
    },
    body: { paddingHorizontal: gap.lg, paddingTop: gap.sm, paddingBottom: gap.lg },
    identity: { alignItems: 'center', paddingTop: gap.xs },
    bigDot: {
      position: 'absolute',
      right: 1,
      bottom: 1,
      width: 20,
      height: 20,
      borderRadius: 10,
      borderWidth: 3,
    },
    statusPill: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 6,
      marginTop: gap.md,
      paddingHorizontal: 13,
      paddingVertical: 7,
      borderRadius: radius.pill,
      maxWidth: '100%',
    },
    pillDot: { width: 6, height: 6, borderRadius: 3 },
    activityCard: {
      backgroundColor: th.cardSunken,
      borderRadius: radius.card,
      padding: 15,
      marginTop: gap.lg,
    },
    track: {
      flex: 1,
      height: 5,
      borderRadius: 99,
      overflow: 'hidden',
    },
    fill: { height: 5, borderRadius: 99 },
    card: {
      backgroundColor: th.card,
      borderRadius: radius.card,
      paddingHorizontal: 14,
      paddingVertical: 4,
      marginTop: gap.sm,
      borderWidth: th.dark ? StyleSheet.hairlineWidth : 0,
      borderColor: th.cardBorder,
    },
    capKnob: {
    width: 26,
    height: 26,
    borderRadius: 13,
    shadowColor: '#000',
    shadowOpacity: 0.15,
    shadowRadius: 2,
    shadowOffset: { width: 0, height: 1 },
    elevation: 2,
  },
  settingRow: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: gap.xs,
      paddingVertical: 14,
      minHeight: 48,
    },
    hair: { height: StyleSheet.hairlineWidth },
    levelRow: {
      flexDirection: 'row',
      alignItems: 'flex-start',
      gap: gap.xs,
      paddingVertical: 10,
      paddingHorizontal: 9,
      borderRadius: 14,
    },
    footer: {
      flexDirection: 'row',
      gap: gap.sm,
      paddingHorizontal: gap.lg,
      paddingTop: gap.sm,
      paddingBottom: gap.xl,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: th.line,
      backgroundColor: th.card,
    },
    btnSecondary: {
      height: 48,
      borderRadius: radius.pill,
      borderWidth: 1,
      alignItems: 'center',
      justifyContent: 'center',
    },
    btnPrimary: {
      height: 48,
      borderRadius: radius.pill,
      alignItems: 'center',
      justifyContent: 'center',
      ...shadow.primary,
    },
  });
