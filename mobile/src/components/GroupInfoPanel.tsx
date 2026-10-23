import React, { useEffect, useRef, useState } from 'react';
import {
  Animated,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
  useWindowDimensions,
} from 'react-native';
import { Avatar } from './Avatar';
import { Button, Icon, IconButton, Input, RadioDot, Row, T, Tag, Toggle, withAlpha } from './ui';
import { humanError } from '../lib/api';
import { useTheme } from '../theme/theme';
import { gap, leading, radius, type } from '../theme/tokens';
import type { AgentActivity, Conversation } from '../lib/types';

/**
 * 聊天信息（设计规范 §6.11）。
 *
 * 从右侧滑入，宽 89%，左侧露出缩小的聊天（聊天同步缩到 0.96）。
 *
 * 内容顺序按使用频率：
 *   群身份卡 → 成员网格（5 列）
 *   → 群名称 / 群公告 → 群应用 → 权限管理（群默认级别 + 成员单独设置）
 *   → 消息免打扰 / 退出（群主是解散）
 *
 * 所有行都是真数据：改名走 PATCH /v1/conversations/{id}，
 * 权限走 defaultPermission，公告取这个群正在做的目标。
 */

export interface PanelMember {
  key: string;
  name: string;
  title?: string;
  color?: string;
}

/** 与 AgentPanel / 新建群页共用同一套级别文案，避免三处各说各话。 */
const LEVELS = [
  { key: 'ask', label: '需要审批', hint: '执行任何操作前，都先征得你同意' },
  { key: 'approval', label: '工作区内修改', hint: '可自由读写工作区；联网、删除、区外操作仍需审批' },
  { key: 'full', label: '完全访问权限', hint: '不再询问，可执行包括删除在内的全部操作', warn: true },
] as const;

function levelLabel(level?: string): string {
  if (!level) return '跟随角色设置';
  return LEVELS.find((l) => l.key === level)?.label ?? level;
}

function statusOf(st?: AgentActivity): { text: string; tone: 'bad' | 'ok' | 'ink' | 'faint' } {
  switch (st?.state) {
    case 'working':
      return { text: '干活中', tone: 'ink' };
    case 'blocked':
      return { text: '等你审批', tone: 'bad' };
    case 'done':
      return { text: '刚完成', tone: 'ok' };
    default:
      return { text: '空闲', tone: 'faint' };
  }
}

export function GroupInfoPanel({
  visible,
  conv,
  members,
  activity,
  goal,
  muted,
  isOwner,
  onClose,
  onOpenMember,
  onAddMember,
  onRename,
  onSetPermission,
  onToggleMute,
  onLeave,
  onOpenLibrary,
  onOpenGoals,
  onOpenConnect,
}: {
  visible: boolean;
  conv: Conversation | null;
  members: PanelMember[];
  activity: AgentActivity[];
  /** 这个群正在做的目标，展示为群公告。 */
  goal: { title: string; status: string } | null;
  muted: boolean;
  isOwner: boolean;
  onClose: () => void;
  onOpenMember: (key: string) => void;
  /** 传入才显示「添加成员」虚线圈；没有可拉的角色时不给假入口。 */
  onAddMember?: () => void;
  onRename: (title: string) => Promise<void>;
  onSetPermission: (level: string) => Promise<void>;
  onToggleMute: (v: boolean) => void;
  onLeave: () => void;
  onOpenLibrary: () => void;
  onOpenGoals: () => void;
  onOpenConnect: () => void;
}) {
  const t = useTheme();
  const { width } = useWindowDimensions();
  const panelW = width * 0.89;
  const slide = useRef(new Animated.Value(1)).current;

  // 关闭时先播完退场动画再卸载，否则面板会「啪」地消失。
  const [mounted, setMounted] = useState(visible);

  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState('');
  const [savingName, setSavingName] = useState(false);
  const [nameErr, setNameErr] = useState('');

  const [savingPerm, setSavingPerm] = useState(false);
  const [permErr, setPermErr] = useState('');
  const [showOverrides, setShowOverrides] = useState(false);
  // 退出/解散用行内二次确认，而不是系统 Alert：Web 预览里 Alert 是空实现。
  const [confirmLeave, setConfirmLeave] = useState(false);

  useEffect(() => {
    if (visible) setMounted(true);
    Animated.spring(slide, {
      toValue: visible ? 0 : 1,
      useNativeDriver: true,
      damping: 26,
      stiffness: 260,
      mass: 0.8,
    }).start(({ finished }) => {
      if (finished && !visible) setMounted(false);
    });
  }, [visible, slide]);

  // 每次打开都回到顶部状态，不留上次的编辑残影。
  useEffect(() => {
    if (!visible) return;
    setEditingName(false);
    setNameErr('');
    setPermErr('');
    setShowOverrides(false);
    setConfirmLeave(false);
  }, [visible]);

  if (!mounted) return null;

  const level = conv?.defaultPermission ?? '';
  const overrides = Object.entries(conv?.agentPermissions ?? {});
  const nameOf = (key: string) => members.find((m) => m.key === key)?.name ?? key;

  const APPS = [
    { icon: 'folder-outline' as const, label: '文件', tint: '#C9A227', onPress: onOpenLibrary },
    { icon: 'image-outline' as const, label: '图片', tint: '#7A9A7E', onPress: onOpenLibrary },
    { icon: 'cube-outline' as const, label: '产物', tint: '#8B7EC8', onPress: onOpenLibrary },
    { icon: 'aperture-outline' as const, label: '目标', tint: '#5F9E72', onPress: onOpenGoals },
    { icon: 'git-network-outline' as const, label: '连接器', tint: '#D96C5F', onPress: onOpenConnect },
  ];

  async function saveName() {
    const next = nameDraft.trim();
    if (!next || next === conv?.title) {
      setEditingName(false);
      return;
    }
    setSavingName(true);
    setNameErr('');
    try {
      await onRename(next);
      setEditingName(false);
    } catch (e) {
      setNameErr(humanError(e));
    } finally {
      setSavingName(false);
    }
  }

  async function pickLevel(next: string) {
    if (savingPerm || next === level) return;
    setSavingPerm(true);
    setPermErr('');
    try {
      await onSetPermission(next);
    } catch (e) {
      setPermErr(humanError(e));
    } finally {
      setSavingPerm(false);
    }
  }

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none">
      <Pressable style={StyleSheet.absoluteFill} onPress={onClose} accessibilityLabel="关闭聊天信息" />
      <Animated.View
        pointerEvents="none"
        style={[
          StyleSheet.absoluteFill,
          {
            backgroundColor: t.scrim,
            opacity: slide.interpolate({ inputRange: [0, 1], outputRange: [1, 0] }),
          },
        ]}
      />
      <Animated.View
        style={[
          s.panel,
          {
            width: panelW,
            backgroundColor: t.canvas[1],
            transform: [
              { translateX: slide.interpolate({ inputRange: [0, 1], outputRange: [0, panelW] }) },
            ],
          },
        ]}
      >
        <View style={[s.header, { borderBottomColor: t.hairline }]}>
          <View style={{ width: 36 }} />
          <T size={type.headline} weight="600" style={s.headerTitle} numberOfLines={1}>
            聊天信息
          </T>
          <IconButton name="close" onPress={onClose} accessibilityLabel="关闭" />
        </View>

        <ScrollView contentContainerStyle={s.body} showsVerticalScrollIndicator={false}>
          {/* 群身份卡 */}
          <View style={[s.card, { backgroundColor: t.card }]}>
            <Row gap={gap.sm}>
              <View style={[s.groupIcon, { backgroundColor: withAlpha('#64766B', 0.14) }]}>
                <Icon name="people" size={26} color="#64766B" />
              </View>
              <View style={{ flex: 1 }}>
                <T size={type.title} weight="600" numberOfLines={1}>
                  {conv?.title ?? '群聊'}
                </T>
                <T size={type.micro} color={t.ink3} numberOfLines={1} style={{ marginTop: 3 }}>
                  纸团号：zt-{(conv?.id ?? '').slice(0, 4) || '----'}
                  {conv?.createdAt ? ` · 建于 ${fmtDate(conv.createdAt)}` : ''}
                </T>
              </View>
              <Icon name="chevron-forward" size={14} color={t.ink4} />
            </Row>
          </View>

          {/* 群成员：5 列网格 */}
          <View style={[s.card, { backgroundColor: t.card }]}>
            <Row style={{ justifyContent: 'space-between', marginBottom: gap.sm }}>
              <T size={type.cardTitle} weight="600">
                群成员
              </T>
              <T size={type.caption} color={t.ink4}>
                {members.length} 人
              </T>
            </Row>
            <View style={s.grid}>
              {members.map((m, i) => {
                const st = activity.find((x) => x.key === m.key);
                const stt = statusOf(st);
                const tone =
                  stt.tone === 'bad'
                    ? t.bad
                    : stt.tone === 'ok'
                      ? t.ok
                      : stt.tone === 'ink'
                        ? t.ink
                        : t.faint;
                return (
                  <Pressable
                    key={m.key}
                    onPress={() => onOpenMember(m.key)}
                    accessibilityRole="button"
                    accessibilityLabel={`${m.name}，${stt.text}`}
                    style={({ pressed }) => [s.memberCell, pressed && { opacity: 0.6 }]}
                  >
                    <Avatar name={m.name} tint={m.color} index={i} size={46} state={st?.state} />
                    <T size={10.5} color={t.ink2} numberOfLines={1} style={{ marginTop: 6 }}>
                      {m.name}
                    </T>
                    <T size={9.5} color={tone} numberOfLines={1} style={{ marginTop: 1 }}>
                      {stt.text}
                    </T>
                  </Pressable>
                );
              })}
              {onAddMember ? (
                <Pressable
                  onPress={onAddMember}
                  accessibilityRole="button"
                  accessibilityLabel="添加成员"
                  style={({ pressed }) => [s.memberCell, pressed && { opacity: 0.6 }]}
                >
                  <View
                    style={[
                      s.addCircle,
                      { borderColor: t.dark ? 'rgba(255,255,255,0.24)' : 'rgba(20,24,40,0.18)' },
                    ]}
                  >
                    <Icon name="add" size={20} color={t.ink4} />
                  </View>
                  <T size={10.5} color={t.ink3} numberOfLines={1} style={{ marginTop: 6 }}>
                    添加成员
                  </T>
                </Pressable>
              ) : null}
            </View>
          </View>

          {/* 群名称 / 群公告 */}
          <View style={[s.card, { backgroundColor: t.card, paddingVertical: 0 }]}>
            {editingName ? (
              <View style={{ paddingVertical: gap.sm, gap: gap.xs }}>
                <T size={type.sub} weight="550">
                  群名称
                </T>
                <Input
                  value={nameDraft}
                  onChangeText={setNameDraft}
                  placeholder="给这个群起个名字"
                  autoFocus
                  style={{ minHeight: 44 }}
                />
                {nameErr ? (
                  <T size={type.micro} color={t.bad} lh={leading.micro}>
                    {nameErr}
                  </T>
                ) : null}
                <Row gap={gap.xs}>
                  <Button
                    label="保存"
                    size="sm"
                    loading={savingName}
                    onPress={saveName}
                    style={{ flex: 1 }}
                  />
                  <Button
                    label="取消"
                    variant="secondary"
                    size="sm"
                    onPress={() => {
                      setEditingName(false);
                      setNameErr('');
                    }}
                    style={{ flex: 1 }}
                  />
                </Row>
              </View>
            ) : (
              <Pressable
                onPress={() => {
                  setNameDraft(conv?.title ?? '');
                  setEditingName(true);
                }}
                accessibilityRole="button"
                accessibilityLabel="修改群名称"
                style={({ pressed }) => [
                  s.line,
                  { borderBottomColor: t.hairline, opacity: pressed ? 0.6 : 1 },
                ]}
              >
                <T size={type.sub} weight="550" style={{ flex: 1 }}>
                  群名称
                </T>
                <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '55%' }}>
                  {conv?.title ?? '—'}
                </T>
                <Icon name="chevron-forward" size={13} color={t.ink4} />
              </Pressable>
            )}

            <Pressable
              onPress={onOpenGoals}
              accessibilityRole="button"
              accessibilityLabel="查看群公告（目标）"
              style={({ pressed }) => [s.line, s.lineLast, { opacity: pressed ? 0.6 : 1 }]}
            >
              <T size={type.sub} weight="550" style={{ flex: 1 }}>
                群公告
              </T>
              <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '55%' }}>
                {goal ? `目标：${goal.title}` : '未设置'}
              </T>
              <Icon name="chevron-forward" size={13} color={t.ink4} />
            </Pressable>
          </View>

          {/* 群应用 */}
          <View style={[s.card, { backgroundColor: t.card }]}>
            <T size={type.cardTitle} weight="600" style={{ marginBottom: gap.sm }}>
              群应用
            </T>
            <Row style={{ justifyContent: 'space-between' }}>
              {APPS.map((a) => (
                <Pressable
                  key={a.label}
                  onPress={a.onPress}
                  accessibilityRole="button"
                  accessibilityLabel={a.label}
                  style={({ pressed }) => [s.appCell, pressed && { opacity: 0.6 }]}
                >
                  <View style={[s.appIcon, { backgroundColor: withAlpha(a.tint, 0.14) }]}>
                    <Icon name={a.icon} size={20} color={a.tint} />
                  </View>
                  <T size={10.5} color={t.ink3} style={{ marginTop: 6 }}>
                    {a.label}
                  </T>
                </Pressable>
              ))}
            </Row>
          </View>

          {/* 权限管理：群默认级别 */}
          <View style={[s.card, { backgroundColor: t.card }]}>
            <Row style={{ justifyContent: 'space-between' }}>
              <T size={type.cardTitle} weight="600">
                权限管理
              </T>
              <T size={type.micro} color={t.ink4}>
                由你给每个成员分配
              </T>
            </Row>
            <T size={type.micro} color={t.ink4} style={{ marginTop: 2 }}>
              本群默认级别（成员可单独设置）
            </T>

            <View style={{ marginTop: gap.sm, gap: 4 }}>
              {LEVELS.map((l) => {
                const on = level === l.key;
                return (
                  <Pressable
                    key={l.key}
                    onPress={() => pickLevel(l.key)}
                    disabled={savingPerm}
                    accessibilityRole="radio"
                    accessibilityState={{ selected: on }}
                    style={[
                      s.levelRow,
                      { backgroundColor: on ? t.cardSunken : 'transparent' },
                      savingPerm && { opacity: 0.6 },
                    ]}
                  >
                    <RadioDot on={on} />
                    <View style={{ flex: 1 }}>
                      <Row gap={6}>
                        <T size={type.sub} weight={on ? '600' : '400'}>
                          {l.label}
                        </T>
                        {'warn' in l && l.warn ? <Tag label="谨慎授予" tone="gold" /> : null}
                      </Row>
                      <T size={type.micro} color={t.ink3} lh={leading.micro} style={{ marginTop: 2 }}>
                        {l.hint}
                      </T>
                    </View>
                  </Pressable>
                );
              })}
            </View>
            {!level ? (
              <T size={type.micro} color={t.ink4} style={{ marginTop: 6 }}>
                当前跟随各角色自己的设置。
              </T>
            ) : null}
            {permErr ? (
              <T size={type.micro} color={t.bad} lh={leading.micro} style={{ marginTop: 6 }}>
                {permErr}
              </T>
            ) : null}

            <Pressable
              onPress={() => setShowOverrides((v) => !v)}
              accessibilityRole="button"
              style={({ pressed }) => [
                s.line,
                { borderTopColor: t.hairline, borderBottomWidth: 0, opacity: pressed ? 0.6 : 1 },
              ]}
            >
              <T size={type.sub} weight="550" style={{ flex: 1 }}>
                成员单独设置
              </T>
              <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '50%' }}>
                {overrides.length === 1
                  ? `${nameOf(overrides[0][0])}：${levelLabel(overrides[0][1])}`
                  : overrides.length
                    ? `${overrides.length} 位已设置`
                    : '未设置'}
              </T>
              <Icon
                name={showOverrides ? 'chevron-down' : 'chevron-forward'}
                size={13}
                color={t.ink4}
              />
            </Pressable>

            {showOverrides ? (
              <View style={{ gap: 2 }}>
                {members.map((m) => {
                  const own = conv?.agentPermissions?.[m.key];
                  return (
                    <Pressable
                      key={m.key}
                      onPress={() => onOpenMember(m.key)}
                      style={({ pressed }) => [s.overrideRow, pressed && { opacity: 0.6 }]}
                    >
                      <Avatar name={m.name} tint={m.color} size={26} showDot={false} />
                      <T size={type.caption} style={{ flex: 1 }} numberOfLines={1}>
                        {m.name}
                      </T>
                      <T size={type.micro} color={own ? t.ink2 : t.ink4}>
                        {own ? levelLabel(own) : '跟随群默认'}
                      </T>
                      <Icon name="chevron-forward" size={12} color={t.ink4} />
                    </Pressable>
                  );
                })}
              </View>
            ) : null}
          </View>

          {/* 免打扰 / 退出 */}
          <View style={[s.card, { backgroundColor: t.card, paddingVertical: 0 }]}>
            <Row style={[s.line, { borderBottomColor: t.hairline }]}>
              <T size={type.sub} weight="550" style={{ flex: 1 }}>
                消息免打扰
              </T>
              <Toggle on={muted} onChange={onToggleMute} />
            </Row>

            {confirmLeave ? (
              <View style={{ paddingVertical: gap.sm, gap: gap.xs }}>
                <T size={type.micro} color={t.ink3} lh={leading.micro}>
                  {isOwner
                    ? `「${conv?.title ?? '这个群'}」的消息和记忆会一起删除，无法恢复。`
                    : `退出后不再收到「${conv?.title ?? '这个群'}」的消息，可以再被邀请回来。`}
                </T>
                <Row gap={gap.xs}>
                  <Button
                    label={isOwner ? '确认解散' : '确认退出'}
                    variant="danger"
                    size="sm"
                    onPress={onLeave}
                    style={{ flex: 1 }}
                  />
                  <Button
                    label="取消"
                    variant="secondary"
                    size="sm"
                    onPress={() => setConfirmLeave(false)}
                    style={{ flex: 1 }}
                  />
                </Row>
              </View>
            ) : (
              <Pressable
                onPress={() => setConfirmLeave(true)}
                accessibilityRole="button"
                style={({ pressed }) => [s.line, s.lineLast, pressed && { opacity: 0.6 }]}
              >
                <T size={type.sub} color={t.bad} weight="550" style={{ flex: 1 }}>
                  {isOwner ? '解散群聊' : '退出群聊'}
                </T>
              </Pressable>
            )}
          </View>

          <T size={type.micro} color={t.faint} align="center" style={{ marginTop: gap.xs }}>
            {isOwner ? '你是群主，可以解散这个群' : '群主可以删除这个群'}
          </T>
        </ScrollView>
      </Animated.View>
    </View>
  );
}

function fmtDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

const s = StyleSheet.create({
  panel: {
    position: 'absolute',
    top: 0,
    bottom: 0,
    right: 0,
    borderTopLeftRadius: 24,
    borderBottomLeftRadius: 24,
    shadowColor: '#101214',
    shadowOpacity: 0.2,
    shadowRadius: 34,
    shadowOffset: { width: -12, height: 0 },
    elevation: 18,
    overflow: 'hidden',
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: gap.xs,
    paddingVertical: gap.xs,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  headerTitle: { flex: 1, textAlign: 'center' },
  body: { padding: gap.sm, gap: 10, paddingBottom: 40 },
  card: { borderRadius: radius.card, padding: 14 },
  groupIcon: {
    width: 54,
    height: 54,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  grid: { flexDirection: 'row', flexWrap: 'wrap', rowGap: 12 },
  memberCell: { width: '20%', alignItems: 'center' },
  addCircle: {
    width: 46,
    height: 46,
    borderRadius: 23,
    borderWidth: 1,
    borderStyle: 'dashed',
    alignItems: 'center',
    justifyContent: 'center',
  },
  appCell: { alignItems: 'center', flex: 1 },
  appIcon: {
    width: 46,
    height: 46,
    borderRadius: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  line: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.xs,
    paddingVertical: 14,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  lineLast: { borderBottomWidth: 0 },
  levelRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: gap.xs,
    paddingVertical: 9,
    paddingHorizontal: 9,
    borderRadius: 14,
  },
  overrideRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.xs,
    paddingVertical: 8,
  },
});
