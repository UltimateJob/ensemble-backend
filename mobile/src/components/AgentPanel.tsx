import React, { useEffect, useState } from 'react';
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
import { Button, Icon, IconButton, Row, T, Tag } from './ui';
import { get, humanError } from '../lib/api';
import type { AgentActivity, Run } from '../lib/types';
import { useTheme } from '../theme/theme';
import { gap, leading, radius, type } from '../theme/tokens';

/**
 * Agent 独立工作面板（从右侧滑出）。
 *
 * 这是「Agent 私有视图」：它正在干什么、干到第几步、之前的活干得怎么样。
 * 草稿默认不推群聊，所以群里保持干净。
 */
export function AgentPanel({
  visible,
  agent,
  activity,
  convId,
  workspaceId,
  onClose,
}: {
  visible: boolean;
  agent: { key: string; name: string; title?: string; color?: string } | null;
  activity?: AgentActivity;
  convId: string;
  workspaceId?: string;
  onClose: () => void;
}) {
  const th = useTheme();
  const s = mkStyles(th);
  const { width, height } = useWindowDimensions();
  const panelH = height * 0.88;
  const slide = React.useRef(new Animated.Value(1)).current;

  const [run, setRun] = useState<Run | null>(null);
  const [history, setHistory] = useState<Run[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

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
    if (!visible || !agent) return;
    let alive = true;
    (async () => {
      setErr('');
      setLoading(true);
      try {
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
  }, [visible, agent, activity?.runId, workspaceId, reloadKey]);

  if (!visible || !agent) return null;

  const stateTone =
    activity?.state === 'done' ? 'done' : activity?.state === 'blocked' ? 'blocked' : 'working';
  const stateLabel =
    activity?.state === 'working'
      ? '执行中'
      : activity?.state === 'blocked'
        ? '待处理'
        : activity?.state === 'done'
          ? '刚完成'
          : '空闲';

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none">
      <Pressable style={s.scrim} onPress={onClose} />
      <Animated.View
        style={[
          s.panel,
          {
            left: 0,
            transform: [
              {
                translateY: slide.interpolate({
                  inputRange: [0, 1],
                  outputRange: [0, panelH],
                }),
              },
            ],
          },
        ]}
      >
        {/* 拖拽手柄：iOS bottom sheet 的标准视觉语言 */}
        <View style={{ alignItems: 'center', paddingTop: 8, paddingBottom: 2 }}>
          <View style={{ width: 36, height: 4, borderRadius: 2, backgroundColor: th.dark ? 'rgba(255,255,255,0.18)' : 'rgba(16,20,45,0.14)' }} />
        </View>
        <View style={s.header}>
          <Row gap={gap.xs} style={{ flex: 1 }}>
            <Avatar name={agent.name} size={52} tint={agent.color} state={activity?.state} />
            <View style={{ flex: 1, marginLeft: 2 }}>
              <T size={type.title} weight="600" numberOfLines={1}>
                {agent.name}
              </T>
              <T size={type.caption} color={th.ink3} numberOfLines={1} style={{ marginTop: 2 }}>
                {agent.title || '团队成员'}
              </T>
            </View>
          </Row>
          <IconButton name="close" onPress={onClose} accessibilityLabel="关闭" />
        </View>

        <ScrollView
          contentContainerStyle={[s.body, { flexGrow: 1 }]}
          showsVerticalScrollIndicator={false}
        >
          {loading ? <ActivityIndicator color={th.accent} style={{ marginTop: gap.md }} /> : null}
          {err ? (
            <T size={type.sub} color={th.bad}>
              {err}
            </T>
          ) : null}

          <Row style={{ justifyContent: 'space-between', marginBottom: gap.xs }}>
            <T size={type.caption} color={th.ink3} weight="600">
              当前任务
            </T>
            <Tag label={stateLabel} tone={stateTone as any} />
          </Row>

          <View style={s.block}>
            {run ? (
              <>
                <T size={type.body} weight="500" lh={leading.body}>
                  {run.task}
                </T>
                <T size={type.caption} color={th.ink3} style={{ marginTop: 6 }}>
                  第 {run.step}/{run.maxSteps} 步
                  {run.origin ? ` · 来自${run.origin.startsWith('device:') ? '手机' : '本机'}` : ''}
                </T>
                {run.toolCalls?.length ? (
                  <View style={{ marginTop: gap.sm, gap: gap.xs }}>
                    {run.toolCalls.slice(-6).map((t: any, i: number) => (
                      <Row key={i} gap={7} align="flex-start">
                        <View style={s.dot} />
                        <T
size={type.caption}
                          color={th.ink2}
                          numberOfLines={1}
                          style={{ flex: 1 }}
                        >
                          {t.name} {t.arg?.path || t.arg?.cmd || ''}
                        </T>
                      </Row>
                    ))}
                  </View>
                ) : null}
                {run.summary ? (
                  <T size={type.sub} color={th.ink2} lh={leading.sub} style={{ marginTop: gap.sm }}>
                    {run.summary}
                  </T>
                ) : null}
                {run.error ? (
                  <T size={type.sub} color={th.bad} lh={leading.sub} style={{ marginTop: gap.sm }}>
                    {run.error}
                  </T>
                ) : null}
              </>
            ) : (
              <T size={type.sub} color={th.ink3} lh={leading.sub}>
                现在没有在跑的任务。它的私活会出现在这里，不会刷到群聊。
              </T>
            )}
          </View>

          <T size={type.caption} color={th.ink3} weight="600" style={{ marginTop: gap.lg }}>
            历史记录
          </T>
          {history.length === 0 ? (
            <T size={type.caption} color={th.ink4} style={{ marginTop: gap.xxs }}>
              还没有历史任务
            </T>
          ) : (
            <View style={{ marginTop: gap.xxs }}>
              {history.map((h, i) => (
                <View key={h.id}>
                  {i > 0 ? <View style={s.histLine} /> : null}
                  <View style={{ paddingVertical: gap.xs }}>
                    <T size={type.sub} numberOfLines={1}>
                      {h.task}
                    </T>
                    <Row gap={5} style={{ marginTop: 3 }}>
                      <Icon name="time-outline" size={11} color={th.ink4} />
                      <T size={type.caption} color={th.ink3}>
                        {runStatusLabel(h.status)} · {fmtTime(h.startedAt)}
                      </T>
                    </Row>
                  </View>
                </View>
              ))}
            </View>
          )}
        </ScrollView>

        <View style={s.footer}>
          <Button label="单独发指令" size="sm" style={{ flex: 1 }} />
          <Button
            label="刷新状态"
            variant="secondary"
            size="sm"
            style={{ flex: 1 }}
            onPress={() => setReloadKey((k) => k + 1)}
          />
        </View>
      </Animated.View>
    </View>
  );
}

function runStatusLabel(s: string): string {
  switch (s) {
    case 'running':
      return '执行中';
    case 'waiting_approval':
      return '等审批';
    case 'done':
      return '已完成';
    case 'failed':
      return '失败';
    case 'cancelled':
      return '已取消';
    case 'aborted':
      return '已中止';
    default:
      return s;
  }
}

function fmtTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  const diff = Date.now() - d.getTime();
  if (diff < 60000) return '刚刚';
  if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`;
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

const mkStyles = (th: any) => StyleSheet.create({
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
    // 底部对齐的 sheet，高度最多 88%：内容少时不会留一大片白
    bottom: 0,
    right: 0,
    maxHeight: '88%',
    minHeight: 320,
    backgroundColor: th.card,
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    // 没有 overflow hidden，子元素的直角会盖住圆角
    overflow: 'hidden',
    shadowColor: th.shadowColor, shadowOpacity: 0.35, shadowRadius: 32, shadowOffset: { width: 0, height: 14 }, elevation: 16,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.xs,
    paddingHorizontal: gap.md,
    paddingTop: gap.lg,
    paddingBottom: gap.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: th.line,
  },
  body: { padding: gap.md, paddingBottom: gap.xl },
  block: {
    backgroundColor: th.cardSunken,
    borderRadius: radius.card,
    padding: gap.md,
  },
  dot: {
    width: 5,
    height: 5,
    borderRadius: 3,
    backgroundColor: th.accent,
    marginTop: 6,
  },
  histLine: { height: StyleSheet.hairlineWidth, backgroundColor: th.line },
  footer: {
    flexDirection: 'row',
    gap: gap.xs,
    padding: gap.md,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: th.line,
  },
});
