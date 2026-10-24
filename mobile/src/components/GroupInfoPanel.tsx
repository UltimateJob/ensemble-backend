import React, { useEffect, useRef, useState } from 'react';
import {
  Animated,
  Easing,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
  useWindowDimensions,
} from 'react-native';
import { Avatar, AvatarStack } from './Avatar';
import {
  Button,
  Card,
  Icon,
  IconButton,
  Input,
  RadioDot,
  Row,
  T,
  Tag,
  Toggle,
  withAlpha,
} from './ui';
import { humanError } from '../lib/api';
import { useTheme } from '../theme/theme';
import {
  CARD_GAP,
  FILE_COLORS,
  gap,
  leading,
  motion,
  radius,
  type,
  weight,
} from '../theme/tokens';
import { paperNo, type AgentActivity, type AgentState, type Conversation } from '../lib/types';

/**
 * 群聊信息面板（设计规范 §6.11）。
 *
 * 从右侧滑入，宽 89%，左侧露出一条聊天。规范要求背景聊天缩到 0.96 并
 * 压暗 8%；本组件挂在群聊页内部，拿不到兄弟节点的 transform，
 * 因此按规范给出的替代方案，用 8% 中性遮罩近似。
 *
 * 内容顺序按使用频率（§6.11）：
 *   群身份卡 → 成员网格（5 列）→ 群名称 / 群公告 → 群应用
 *   → 权限管理（§7：群默认三级 + 成员单独设置）→ 免打扰 / 退出
 *
 * 所有卡片圆角 24、间距 10，与主页一致。
 */

export interface PanelMember {
  key: string;
  name: string;
  title?: string;
  color?: string;
  /** 父页面已经算好序号与状态时直接用，省一次查找。 */
  index?: number;
  state?: AgentState;
}

interface Level {
  key: string;
  label: string;
  hint: string;
  warn?: boolean;
}

/** 与 AgentPanel / 新建群页共用同一套级别文案，避免三处各说各话。 */
const LEVELS: Level[] = [
  { key: 'ask', label: '需要审批', hint: '执行任何操作前，都先征得你同意' },
  {
    key: 'approval',
    label: '工作区内修改',
    hint: '可自由读写工作区内的文件；联网、删除、区外操作仍需审批',
  },
  {
    key: 'full',
    label: '完全访问权限',
    hint: '不再询问，可执行包括删除在内的全部操作',
    warn: true,
  },
];

function levelLabel(level?: string): string {
  if (!level) return '跟随群默认';
  if (level === 'readonly') return '只读';
  return LEVELS.find((l) => l.key === level)?.label ?? level;
}

function statusOf(st?: AgentState): {
  text: string;
  tone: 'ok' | 'bad' | 'ink' | 'idle';
} {
  switch (st) {
    case 'working':
      return { text: '干活中', tone: 'ink' };
    case 'blocked':
      return { text: '等你审批', tone: 'bad' };
    case 'done':
      return { text: '刚完成', tone: 'ok' };
    default:
      return { text: '空闲', tone: 'idle' };
  }
}

export function GroupInfoPanel({
  visible,
  conv,
  members,
  activity,
  goal,
  isOwner,
  onClose,
  onOpenMember,
  onAddMember,
  onRename,
  onSetPermission,
  onLeave,
  onOpenLibrary,
  onOpenGoals,
  onOpenConnect,
  onOpenTask,
}: {
  visible: boolean;
  conv: Conversation | null;
  members: PanelMember[];
  activity: AgentActivity[];
  /** 这个群正在做的目标，展示为群公告。 */
  goal: { title: string; status: string } | null;
  isOwner: boolean;
  onClose: () => void;
  onOpenMember: (key: string) => void;
  /** 传入才显示「添加」虚线圈；没有可拉的角色时不给假入口。 */
  onAddMember?: () => void;
  onRename: (title: string) => Promise<void>;
  onSetPermission: (level: string) => Promise<void>;
  onLeave: () => void;
  onOpenLibrary: () => void;
  onOpenGoals: () => void;
  onOpenConnect: () => void;
  onOpenTask: () => void;
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
  // 后端未及时回写标题时，先用本地值撑住界面。
  const [titleOverride, setTitleOverride] = useState<string | null>(null);

  const [permErr, setPermErr] = useState('');
  const [showOverrides, setShowOverrides] = useState(false);
  // 退出/解散用行内二次确认，而不是系统 Alert：Web 预览里 Alert 是空实现。
  const [confirmLeave, setConfirmLeave] = useState(false);

  const serverLevel = conv?.defaultPermission ?? '';
  const [draftLevel, setDraftLevel] = useState(serverLevel);

  useEffect(() => {
    if (visible) setMounted(true);
    // §8：380ms，cubic-bezier(.32,.72,.24,1) 近似弹簧。
    Animated.timing(slide, {
      toValue: visible ? 0 : 1,
      duration: motion.panel,
      easing: Easing.bezier(0.32, 0.72, 0.24, 1),
      useNativeDriver: true,
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
    setTitleOverride(null);
  }, [visible]);

  useEffect(() => setDraftLevel(serverLevel), [serverLevel]);

  if (!mounted) return null;

  const shownTitle = titleOverride ?? conv?.title ?? '群聊';
  const overrides = Object.entries(conv?.agentPermissions ?? {});
  const nameOf = (key: string) => members.find((m) => m.key === key)?.name ?? key;
  const stateOf = (m: PanelMember): AgentState | undefined =>
    m.state ?? activity.find((a) => a.key === m.key)?.state;

  // 群号用和账号同一套规则，只是种子换成会话 ID，避免两套算法对不上。
  const ztId = paperNo(conv?.id);

  const APPS = [
    // 「交给团队」放第一个：这是这个群里最重的操作，其余都是看结果。
    { icon: 'rocket-outline' as const, label: '交给团队', tint: FILE_COLORS.goal, onPress: onOpenTask },
    { icon: 'folder-outline' as const, label: '文件', tint: FILE_COLORS.doc, onPress: onOpenLibrary },
    { icon: 'image-outline' as const, label: '图片', tint: FILE_COLORS.image, onPress: onOpenLibrary },
    { icon: 'cube-outline' as const, label: '产物', tint: FILE_COLORS.artifact, onPress: onOpenLibrary },
    { icon: 'aperture-outline' as const, label: '目标', tint: FILE_COLORS.goal, onPress: onOpenGoals },
    {
      icon: 'git-network-outline' as const,
      label: '连接器',
      tint: FILE_COLORS.connector,
      onPress: onOpenConnect,
    },
  ];

  async function saveName() {
    const next = nameDraft.trim();
    if (!next || next === shownTitle) {
      setEditingName(false);
      return;
    }
    setSavingName(true);
    setNameErr('');
    try {
      await onRename(next);
      setTitleOverride(next);
      setEditingName(false);
    } catch (e) {
      setNameErr(humanError(e));
    } finally {
      setSavingName(false);
    }
  }

  async function pickLevel(next: string) {
    if (next === draftLevel) return;
    const prev = draftLevel;
    // 先动 UI 再等接口，点下去就有反馈；失败再退回原级别。
    setDraftLevel(next);
    setPermErr('');
    try {
      await onSetPermission(next);
    } catch (e) {
      setDraftLevel(prev);
      setPermErr(humanError(e));
    }
  }

  const overrideSummary =
    overrides.length === 1
      ? `${nameOf(overrides[0][0])}：${levelLabel(overrides[0][1])}`
      : overrides.length
        ? `${overrides.length} 位已设置`
        : '未设置';

  return (
    <View style={StyleSheet.absoluteFill} pointerEvents="box-none">
      <Pressable
        style={StyleSheet.absoluteFill}
        onPress={onClose}
        accessibilityLabel="关闭聊天信息"
      />

      {/* §6.11 / §8：背景压暗 8%。t.scrim 本身带透明度，用低 opacity 叠出 8%。 */}
      <Animated.View
        pointerEvents="none"
        style={[
          StyleSheet.absoluteFill,
          {
            backgroundColor: t.scrim,
            opacity: slide.interpolate({
              inputRange: [0, 1],
              outputRange: [t.dark ? 0.14 : 0.21, 0],
            }),
          },
        ]}
      />

      <Animated.View
        pointerEvents="auto"
        style={[
          s.panel,
          {
            width: panelW,
            backgroundColor: t.canvas[0],
            shadowColor: t.shadowColor,
            transform: [
              { translateX: slide.interpolate({ inputRange: [0, 1], outputRange: [0, panelW] }) },
            ],
          },
        ]}
      >
        <View style={[s.header, { borderBottomColor: t.hairline }]}>
          <View style={{ width: 36 }} />
          <T size={type.title} weight={weight.strong} style={s.headerTitle} numberOfLines={1}>
            聊天信息
          </T>
          <IconButton name="close" onPress={onClose} accessibilityLabel="关闭" />
        </View>

        <ScrollView contentContainerStyle={s.body} showsVerticalScrollIndicator={false}>
          {/* 群身份卡：头像组 + 群名 + 纸团号 / 建群日 */}
          <Card padded={false} style={s.card}>
            <Row gap={gap.md}>
              <View style={[s.groupTile, { backgroundColor: t.cardSunken }]}>
                <AvatarStack
                  names={members.slice(0, 3).map((m) => m.name)}
                  tints={members.slice(0, 3).map((m) => m.color ?? '')}
                  size={26}
                  max={3}
                />
              </View>
              <View style={{ flex: 1 }}>
                <T size={type.title} weight={weight.strong} numberOfLines={1}>
                  {shownTitle}
                </T>
                <T size={type.micro} color={t.ink3} numberOfLines={1} style={{ marginTop: 4 }}>
                  纸团号 {ztId}
                  {conv?.createdAt ? ` · 建于 ${fmtDate(conv.createdAt)}` : ''}
                </T>
              </View>
            </Row>
          </Card>

          {/* 群成员：5 列网格，46 头像 + 状态点 + 状态文案 */}
          <Card padded={false} style={s.card}>
            <Row style={{ justifyContent: 'space-between', marginBottom: gap.sm }}>
              <T size={type.cardTitle} weight={weight.strong}>
                群成员
              </T>
              <T size={type.caption} color={t.ink4}>
                {members.length} 人
              </T>
            </Row>
            <View style={s.grid}>
              {members.map((m, i) => {
                const state = stateOf(m);
                const stt = statusOf(state);
                const tone =
                  stt.tone === 'bad'
                    ? t.bad
                    : stt.tone === 'ok'
                      ? t.ok
                      : stt.tone === 'ink'
                        ? t.ink
                        : t.ink4;
                const dot =
                  state === 'blocked'
                    ? t.dangerDot
                    : state === 'done'
                      ? t.okDot
                      : state === 'working'
                        ? t.ink
                        : null;
                return (
                  <Pressable
                    key={m.key}
                    onPress={() => onOpenMember(m.key)}
                    accessibilityRole="button"
                    accessibilityLabel={`${m.name}，${stt.text}`}
                    style={({ pressed }) => [s.memberCell, pressed && { opacity: 0.6 }]}
                  >
                    <View style={{ width: 46, height: 46 }}>
                      <Avatar name={m.name} tint={m.color} index={m.index ?? i} size={46} />
                      {dot ? (
                        <View
                          style={[
                            s.stateDot,
                            { backgroundColor: dot, borderColor: t.card },
                          ]}
                        />
                      ) : null}
                    </View>
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
                      {
                        borderColor: t.dark
                          ? withAlpha(t.white, 0.24)
                          : withAlpha(t.ink, 0.18),
                      },
                    ]}
                  >
                    <Icon name="add" size={20} color={t.ink4} />
                  </View>
                  <T size={10.5} color={t.ink3} numberOfLines={1} style={{ marginTop: 6 }}>
                    添加
                  </T>
                </Pressable>
              ) : null}
            </View>
          </Card>

          {/* 群名称 / 群公告 */}
          <Card padded={false} style={[s.card, s.cardLines]}>
            {editingName ? (
              <View style={{ paddingVertical: gap.sm, gap: gap.xs }}>
                <T size={type.sub} weight={weight.medium}>
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
                  <Button label="保存" size="sm" loading={savingName} onPress={saveName} style={{ flex: 1 }} />
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
                  setNameDraft(shownTitle);
                  setEditingName(true);
                }}
                accessibilityRole="button"
                accessibilityLabel="修改群名称"
                style={({ pressed }) => [
                  s.line,
                  { borderBottomColor: t.hairline, opacity: pressed ? 0.6 : 1 },
                ]}
              >
                <T size={type.sub} weight={weight.medium} style={{ flex: 1 }}>
                  群名称
                </T>
                <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '55%' }}>
                  {shownTitle}
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
              <T size={type.sub} weight={weight.medium} style={{ flex: 1 }}>
                群公告
              </T>
              <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '55%' }}>
                {goal ? `目标：${goal.title}` : '未设置'}
              </T>
              <Icon name="chevron-forward" size={13} color={t.ink4} />
            </Pressable>
          </Card>

          {/* 群应用：46 圆角 14 低饱和图标底（FILE_COLORS） */}
          <Card padded={false} style={s.card}>
            <T size={type.cardTitle} weight={weight.strong} style={{ marginBottom: gap.sm }}>
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
          </Card>

          {/* 权限管理（§7）：群默认三级 + 成员单独设置入口 */}
          <Card padded={false} style={s.card}>
            <Row style={{ justifyContent: 'space-between' }}>
              <T size={type.cardTitle} weight={weight.strong}>
                权限管理
              </T>
              <T size={type.micro} color={t.ink4}>
                由你给每个成员分配
              </T>
            </Row>
            <T size={type.micro} color={t.ink4} style={{ marginTop: 3 }}>
              本群默认级别（成员可单独设置）
            </T>

            <View style={{ marginTop: gap.sm, gap: 4 }}>
              {LEVELS.map((l) => {
                const on = draftLevel === l.key;
                return (
                  <Pressable
                    key={l.key}
                    onPress={() => pickLevel(l.key)}
                    accessibilityRole="radio"
                    accessibilityState={{ selected: on }}
                    style={({ pressed }) => [
                      s.levelRow,
                      { backgroundColor: on ? t.cardSunken : 'transparent' },
                      pressed && { opacity: 0.7 },
                    ]}
                  >
                    <RadioDot on={on} />
                    <View style={{ flex: 1 }}>
                      <Row gap={6}>
                        <T size={type.sub} weight={on ? weight.semibold : weight.regular}>
                          {l.label}
                        </T>
                        {l.warn ? <Tag label="谨慎授予" tone="gold" /> : null}
                      </Row>
                      <T size={type.micro} color={t.ink3} lh={leading.micro} style={{ marginTop: 2 }}>
                        {l.hint}
                      </T>
                    </View>
                  </Pressable>
                );
              })}
            </View>

            {!serverLevel && !draftLevel ? (
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
              accessibilityState={{ expanded: showOverrides }}
              style={({ pressed }) => [
                s.line,
                {
                  borderTopColor: t.hairline,
                  borderBottomWidth: 0,
                  marginTop: gap.sm,
                  opacity: pressed ? 0.6 : 1,
                },
              ]}
            >
              <T size={type.sub} weight={weight.medium} style={{ flex: 1 }}>
                成员单独设置
              </T>
              <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '50%' }}>
                {overrideSummary}
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
                      accessibilityRole="button"
                      accessibilityLabel={`${m.name}，${own ? levelLabel(own) : '跟随群默认'}`}
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
          </Card>

          {/* 退出群聊。免打扰在推送通道接上之前是空开关，先不摆出来。 */}
          <Card padded={false} style={[s.card, s.cardLines]}>
            {confirmLeave ? (
              <View style={{ paddingVertical: gap.sm, gap: gap.xs }}>
                <T size={type.micro} color={t.ink3} lh={leading.micro}>
                  {isOwner
                    ? `「${shownTitle}」的消息和记忆会一起删除，无法恢复。`
                    : `退出后不再收到「${shownTitle}」的消息，可以再被邀请回来。`}
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
                <T size={type.sub} color={t.bad} weight={weight.medium} style={{ flex: 1 }}>
                  {isOwner ? '解散群聊' : '退出群聊'}
                </T>
              </Pressable>
            )}
          </Card>
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
  body: { padding: gap.sm, gap: CARD_GAP, paddingBottom: 40 },
  card: { borderRadius: radius.card, padding: 14 },
  cardLines: { paddingTop: 0, paddingBottom: 0 },
  groupTile: {
    // 三个 26px 头像叠放后宽约 74px。之前写 64，容器不够宽，
    // 头像组被挤成竖排，看起来像三个孤立的方块。
    width: 84,
    height: 64,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
  },
  grid: { flexDirection: 'row', flexWrap: 'wrap', rowGap: 12 },
  memberCell: { width: '20%', alignItems: 'center' },
  stateDot: {
    position: 'absolute',
    right: -1,
    bottom: -1,
    width: 13,
    height: 13,
    borderRadius: 7,
    borderWidth: 2,
  },
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
    minHeight: 48,
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
