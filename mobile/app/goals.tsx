import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import Svg, { Defs, LinearGradient as SvgGradient, Stop, Text as SvgText } from 'react-native-svg';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AvatarStack } from '../src/components/Avatar';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { ListSkeleton } from '../src/components/Skeleton';
import { EmptyGoals } from '../src/components/Illustration';
import {
  Card,
  Empty,
  Icon,
  IconButton,
  Input,
  NavBar,
  Progress,
  Row,
  T,
  TextButton,
} from '../src/components/ui';
import { get, humanError, patch, post } from '../src/lib/api';
import { useApp } from '../src/lib/store';
import type { Conversation, Goal, GoalStatus } from '../src/lib/types';
import { useTheme } from '../src/theme/theme';
import { fontFamily } from '../src/theme/fonts';
import {
  CARD_GAP,
  CARD_PAD,
  MEMBER_COLORS,
  PAGE_X,
  gap,
  radius,
  type,
  weight,
} from '../src/theme/tokens';

/**
 * 目标（设计规范附录 C 第 12 屏 / v18-phone5）。
 *
 * 一张目标卡讲完三件事：
 *   1. 走到哪了 —— 大号渐变百分比 + 进度条
 *   2. 卡在哪 —— 步骤清单三态（完成墨黑勾 / 等待琥珀点 / 未开始灰圈）
 *   3. 谁在做 —— 底部成员头像组 + 当前执行句
 *
 * 步骤不是装饰：点一下就 PATCH `{steps}` 回去，后端据此派生目标状态。
 */

type StepStatus = 'done' | 'waiting' | 'todo';

interface GoalStep {
  id: string;
  title: string;
  status: StepStatus;
  owner?: string;
}

type GoalView = Goal & { steps?: GoalStep[] };

const STATUS_ORDER: Record<GoalStatus, number> = { doing: 0, todo: 1, done: 2 };

export default function Goals() {
  const t = useTheme();
  const router = useRouter();
  const convs = useApp((s) => s.convs);

  const [goals, setGoals] = useState<GoalView[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [err, setErr] = useState('');
  const [adding, setAdding] = useState(false);
  const [title, setTitle] = useState('');
  const [creating, setCreating] = useState(false);

  const load = useCallback(async () => {
    setErr('');
    try {
      const list = await get<GoalView[]>('/v1/goals');
      setGoals(Array.isArray(list) ? list : []);
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const sorted = useMemo(
    () =>
      [...goals].sort(
        (a, b) =>
          STATUS_ORDER[a.status] - STATUS_ORDER[b.status] ||
          +new Date(b.updatedAt) - +new Date(a.updatedAt),
      ),
    [goals],
  );
  const primary = sorted[0];
  const rest = sorted.slice(1);

  /* ---- 群成员：assignees 存角色 key，展示要用名字 ---- */
  const convOf = (g: Goal) => convs.find((c) => c.id === g.conversationId);
  // 内置场景的群在库里没有 agents——后端列表接口会把解析结果
  // 放在 resolvedAgents 里，两个都查一遍。
  const agentsOf = (c: Conversation | undefined): { key: string; name: string; color?: string }[] =>
    c?.agents?.length ? c.agents : (((c as any)?.resolvedAgents ?? []) as any[]);
  const nameOf = (c: Conversation | undefined, key?: string) =>
    (key && agentsOf(c).find((a) => a.key === key)?.name) || key || '统筹';
  const namesOf = (g: Goal) => (g.assignees ?? []).map((k) => nameOf(convOf(g), k));
  const leaderOf = (g: Goal) => {
    const c = convOf(g);
    return nameOf(c, g.assignees?.[0] ?? c?.agents?.[0]?.key);
  };
  // 目标可能还没关联到群。这时用所有会话里出现过的角色拼一张
  // key→name 的全局表——比直接显示 `scribe` 这种内部 key 好得多。
  const globalAgents = React.useMemo(() => {
    const m = new Map<string, string>();
    for (const c of convs) {
      for (const a of agentsOf(c)) m.set(a.key, a.name);
    }
    return m;
  }, [convs]);
  const ownerOf = (g: Goal) => (step?: GoalStep) => {
    const key = step?.owner ?? g.assignees?.[0];
    if (!key) return '统筹';
    const c = convOf(g);
    const local = agentsOf(c).find((a) => a.key === key)?.name;
    return local ?? globalAgents.get(key) ?? key;
  };

  const activity = useMemo(() => {
    type Ev = { id: string; who: string; what: string; at: string; tone: 'wait' | 'brand' };
    const out: Ev[] = [];
    for (const g of sorted) {
      for (const st of g.steps ?? []) {
        if (st.status !== 'waiting') continue;
        out.push({
          id: `${g.id}-${st.id}`,
          who: ownerOf(g)(st),
          what: `把「${st.title}」提交给你审批`,
          at: g.updatedAt,
          tone: 'wait',
        });
      }
      out.push({
        id: `${g.id}-created`,
        who: leaderOf(g),
        what: '创建了这个目标并完成分工',
        at: g.createdAt,
        tone: 'brand',
      });
    }
    return out.sort((a, b) => +new Date(b.at) - +new Date(a.at)).slice(0, 5);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sorted, convs]);

  async function create() {
    if (!title.trim() || creating) return;
    setCreating(true);
    try {
      await post<Goal>('/v1/goals', { title: title.trim(), status: 'todo' });
      setTitle('');
      setAdding(false);
      await load();
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setCreating(false);
    }
  }

  /** 点步骤：完成 ↔ 未开始。等待中的步骤点一下也按「已处理」算。 */
  async function toggleStep(g: GoalView, st: GoalStep) {
    const next = (g.steps ?? []).map((x) =>
      x.id === st.id ? { ...x, status: (st.status === 'done' ? 'todo' : 'done') as StepStatus } : x,
    );
    // 乐观更新，失败再回滚
    setGoals((l) => l.map((x) => (x.id === g.id ? { ...x, steps: next } : x)));
    try {
      const updated = await patch<GoalView>(`/v1/goals/${g.id}`, { steps: next });
      setGoals((l) => l.map((x) => (x.id === g.id ? { ...x, ...updated } : x)));
    } catch (e) {
      setErr(humanError(e));
      load();
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top']}>
        <NavBar
          title="目标"
          onBack={() => router.back()}
          right={
            <IconButton
              name={adding ? 'close' : 'add'}
              bg="transparent"
              onPress={() => setAdding((v) => !v)}
              accessibilityLabel={adding ? '取消新建' : '新建目标'}
            />
          }
        />
        <ScrollView
          contentContainerStyle={s.page}
          showsVerticalScrollIndicator={false}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              onRefresh={async () => {
                setRefreshing(true);
                await load();
                setRefreshing(false);
              }}
              tintColor={t.accent}
            />
          }
        >
          {adding ? (
            <Card style={{ marginBottom: CARD_GAP, padding: CARD_PAD }}>
              <View style={{ gap: gap.sm }}>
                <Input
                  value={title}
                  onChangeText={setTitle}
                  placeholder="目标标题，如：把 v1.2 发布说明做完"
                  autoFocus
                />
                <Row style={{ justifyContent: 'flex-end' }} gap={gap.xl}>
                  <TextButton label="取消" onPress={() => setAdding(false)} />
                  <TextButton
                    label={creating ? '创建中…' : '创建'}
                    onPress={create}
                    disabled={creating || !title.trim()}
                    fontWeight={weight.strong}
                  />
                </Row>
              </View>
            </Card>
          ) : null}

          {err ? (
            <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
              {err}
            </T>
          ) : null}

          {loading ? (
            <ListSkeleton rows={3} />
          ) : sorted.length === 0 ? (
            <Empty
              illustration={<EmptyGoals size={120} />}
              title="还没有目标"
              hint="目标把群聊和真实产出连起来：进度来自步骤和任务，不是手填的数字"
            />
          ) : (
            <>
              {primary ? (
                <FadeInUp>
                  <PrimaryGoalCard
                    goal={primary}
                    teamName={convOf(primary)?.title}
                    leader={leaderOf(primary)}
                    members={namesOf(primary)}
                    ownerOf={ownerOf(primary)}
                    onToggleStep={toggleStep}
                  />
                </FadeInUp>
              ) : null}

              {rest.map((g, i) => (
                <FadeInUp key={g.id} delay={40 + i * 30}>
                  <SecondaryGoalCard
                    goal={g}
                    teamName={convOf(g)?.title}
                    onToggleStep={toggleStep}
                  />
                </FadeInUp>
              ))}

              {activity.length ? (
                <>
                  <T
                    size={type.caption}
                    color={t.ink3}
                    weight={weight.medium}
                    style={{ marginTop: gap.xxl, marginBottom: gap.sm, marginLeft: gap.xxs }}
                  >
                    目标动态
                  </T>
                  <Card style={{ padding: CARD_PAD }}>
                    <View style={{ gap: 13 }}>
                      {activity.map((e) => (
                        <View key={e.id} style={{ flexDirection: 'row', alignItems: 'flex-start' }}>
                          <View
                            style={{
                              width: 6,
                              height: 6,
                              borderRadius: 3,
                              marginTop: 7,
                              marginRight: 9,
                              backgroundColor: e.tone === 'wait' ? t.warn : t.accent,
                            }}
                          />
                          <T size={13} color={t.ink2} lh={20} style={{ flex: 1 }}>
                            <T size={13} color={t.ink} weight={weight.strong}>
                              {e.who}
                            </T>
                            {e.what}
                            <T size={12} color={t.ink4}>
                              {' · '}
                              {hhmm(e.at)}
                            </T>
                          </T>
                        </View>
                      ))}
                    </View>
                  </Card>
                </>
              ) : null}
            </>
          )}
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   主卡
   ============================================================ */

function PrimaryGoalCard({
  goal,
  teamName,
  leader,
  members,
  ownerOf,
  onToggleStep,
}: {
  goal: GoalView;
  teamName?: string;
  leader: string;
  members: string[];
  ownerOf: (step?: GoalStep) => string;
  onToggleStep: (g: GoalView, s: GoalStep) => void;
}) {
  const t = useTheme();
  const steps = goal.steps ?? [];
  const waiting = steps.find((s) => s.status === 'waiting');
  const elapsed = elapsedMinutes(goal);
  const footer =
    goal.status === 'done'
      ? `已完成 · 用时 ${elapsed} 分钟`
      : goal.status === 'todo'
        ? '待规划 · 还没分给成员'
        : waiting
          ? `${ownerOf(waiting)}执行中 · 已用 ${elapsed} 分钟`
          : `${leader}正在推进 · 已用 ${elapsed} 分钟`;

  return (
    <Card style={{ marginBottom: CARD_GAP, padding: CARD_PAD }}>
      <Row style={{ justifyContent: 'space-between', alignItems: 'center' }}>
        <T size={10.5} weight={weight.bold} color={t.ink3} tracking={2.2}>
          CURRENT GOAL
        </T>
        <StatusPill status={goal.status} />
      </Row>

      <Row
        style={{ justifyContent: 'space-between', alignItems: 'flex-start', marginTop: 6 }}
        gap={gap.md}
      >
        <View style={{ flex: 1 }}>
          <T size={18.5} weight={weight.bold} lh={25} numberOfLines={2}>
            {goal.title}
          </T>
          <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 4 }}>
            {[teamName, `${leader}分工`].filter(Boolean).join(' · ')}
          </T>
        </View>
        <GradNum value={goal.progress} size={40} />
      </Row>

      <View style={{ marginTop: 14 }}>
        <Progress value={goal.progress} />
      </View>

      {steps.length ? (
        <View style={{ marginTop: 17 }}>
          {steps.map((st, i) => (
            <StepRow
              key={st.id}
              step={st}
              last={i === steps.length - 1}
              onPress={() => onToggleStep(goal, st)}
            />
          ))}
        </View>
      ) : null}

      <View
        style={{
          height: StyleSheet.hairlineWidth,
          backgroundColor: t.line,
          marginTop: 16,
          marginBottom: 13,
        }}
      />
      <Row gap={gap.sm}>
        {members.length ? (
          <AvatarStack
            names={members}
            size={24}
            max={5}
            tints={members.map((_, i) => MEMBER_COLORS[i % MEMBER_COLORS.length])}
          />
        ) : null}
        <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ flex: 1 }}>
          {footer}
        </T>
      </Row>
    </Card>
  );
}

/* ============================================================
   次要卡
   ============================================================ */

function SecondaryGoalCard({
  goal,
  teamName,
  onToggleStep,
}: {
  goal: GoalView;
  teamName?: string;
  onToggleStep: (g: GoalView, s: GoalStep) => void;
}) {
  const t = useTheme();
  const [open, setOpen] = useState(false);
  const steps = goal.steps ?? [];

  return (
    <Card
      style={{ marginBottom: CARD_GAP, padding: CARD_PAD }}
      onPress={steps.length ? () => setOpen((v) => !v) : undefined}
    >
      <Row style={{ justifyContent: 'space-between' }}>
        <StatusPill status={goal.status} />
        {teamName ? (
          <T size={type.caption} color={t.ink4} numberOfLines={1} style={{ maxWidth: '58%' }}>
            {teamName}
          </T>
        ) : null}
      </Row>
      <Row style={{ justifyContent: 'space-between', alignItems: 'flex-end', marginTop: 10 }}>
        <T
          size={17}
          weight={weight.strong}
          lh={23}
          numberOfLines={2}
          style={{ flex: 1, paddingRight: gap.md }}
        >
          {goal.title}
        </T>
        <GradNum value={goal.progress} size={22} />
      </Row>
      <View style={{ marginTop: 12 }}>
        <Progress value={goal.progress} />
      </View>

      {open && steps.length ? (
        <View style={{ marginTop: 16 }}>
          {steps.map((st, i) => (
            <StepRow
              key={st.id}
              step={st}
              last={i === steps.length - 1}
              onPress={() => onToggleStep(goal, st)}
            />
          ))}
        </View>
      ) : null}
    </Card>
  );
}

/* ============================================================
   状态标 / 步骤 / 渐变数字
   ============================================================ */

function StatusPill({ status }: { status: GoalStatus }) {
  const t = useTheme();
  const spec =
    status === 'done'
      ? { label: '已完成', bg: t.okSoft, fg: t.ok, border: 'transparent' as string }
      : status === 'doing'
        ? { label: '进行中', bg: t.cardSunken, fg: t.ink2, border: 'transparent' as string }
        : { label: '待规划', bg: 'transparent', fg: t.ink3, border: t.line };
  return (
    <View
      style={{
        alignSelf: 'flex-start',
        flexDirection: 'row',
        alignItems: 'center',
        gap: 4,
        paddingHorizontal: 9,
        paddingVertical: 4,
        borderRadius: radius.pill,
        backgroundColor: spec.bg,
        borderWidth: 1,
        borderColor: spec.border,
      }}
    >
      {status === 'done' ? <Icon name="checkmark" size={11} color={spec.fg} /> : null}
      <T size={11} weight={weight.semibold} color={spec.fg}>
        {spec.label}
      </T>
    </View>
  );
}

/**
 * 步骤行。
 *
 * 三态对应规范：
 *   done    → 墨黑实心圆 + 白勾
 *   waiting → 琥珀描边圆 + 琥珀点，右侧红字「等你审批」
 *   todo    → 灰圈
 * 步骤之间用一条 1.5px 竖线连接，线从圆点下方一直延伸到下一行的圆点上方。
 */
function StepRow({
  step,
  last,
  onPress,
}: {
  step: GoalStep;
  last: boolean;
  onPress: () => void;
}) {
  const t = useTheme();
  const done = step.status === 'done';
  const waiting = step.status === 'waiting';

  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`${step.title}${waiting ? '，等你审批' : done ? '，已完成' : '，未开始'}`}
      style={({ pressed }) => ({ opacity: pressed ? 0.6 : 1 })}
    >
      <View style={{ flexDirection: 'row', alignItems: 'stretch' }}>
        <View style={{ width: 22, alignItems: 'center' }}>
          <StepDot status={step.status} />
          {!last ? (
            <View
              style={{
                width: 1.5,
                flex: 1,
                marginTop: 2,
                marginBottom: 2,
                borderRadius: 1,
                backgroundColor: t.track,
              }}
            />
          ) : null}
        </View>
        <View
          style={{
            flex: 1,
            minHeight: 22,
            marginLeft: 12,
            paddingBottom: last ? 0 : 18,
            flexDirection: 'row',
            alignItems: 'flex-start',
          }}
        >
          <T
            size={14.5}
            weight={done || waiting ? weight.strong : weight.regular}
            color={done ? t.ink3 : waiting ? t.ink : t.ink3}
            lh={20}
            numberOfLines={2}
            style={{ flex: 1 }}
          >
            {step.title}
          </T>
          {waiting ? (
            <T size={12.5} weight={weight.semibold} color={t.bad} style={{ marginLeft: gap.sm }}>
              等你审批
            </T>
          ) : null}
        </View>
      </View>
    </Pressable>
  );
}

function StepDot({ status }: { status: StepStatus }) {
  const t = useTheme();
  if (status === 'done') {
    return (
      <View
        style={{
          width: 22,
          height: 22,
          borderRadius: 11,
          backgroundColor: t.primaryBg,
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Icon name="checkmark" size={13} color={t.primaryInk} />
      </View>
    );
  }
  if (status === 'waiting') {
    return (
      <View
        style={{
          width: 22,
          height: 22,
          borderRadius: 11,
          borderWidth: 2,
          borderColor: t.warn,
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: t.warnSoft,
        }}
      >
        <View style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: t.warn }} />
      </View>
    );
  }
  return (
    <View
      style={{
        width: 22,
        height: 22,
        borderRadius: 11,
        borderWidth: 1.5,
        borderColor: t.radioOff,
      }}
    />
  );
}

/**
 * 渐变数字。
 *
 * 用 SVG 文本而不是 MaskedView：MaskedView 在 Web 上没有实现，
 * 而这个 App 同时跑在 Web 和原生上。SVG 两端都能画。
 */
function GradNum({ value, size }: { value: number; size: number }) {
  const t = useTheme();
  const text = `${Math.round(value)}%`;
  // 估宽要给足：SVG 会裁掉画布外的部分，估小了数字左边会被切。
  // 中文界面里数字用 Geist，字面窄于 0.62em；按 0.72 留余量。
  const width = Math.ceil(
    Array.from(text).reduce((w, ch) => w + size * (ch === '%' ? 1 : 0.72), 0),
  );
  const height = Math.round(size * 1.2);
  const id = `goal-grad-${size}`;

  return (
    <Svg width={width} height={height}>
      <Defs>
        <SvgGradient id={id} x1="0" y1="0" x2="1" y2="1">
          <Stop offset="0" stopColor={t.accent} />
          <Stop offset="1" stopColor={t.accentDeep} />
        </SvgGradient>
      </Defs>
      <SvgText
        x={width}
        y={Math.round(size * 0.98)}
        textAnchor="end"
        fontSize={size}
        fontWeight="700"
        fontFamily={fontFamily('700')}
        fill={`url(#${id})`}
      >
        {text}
      </SvgText>
    </Svg>
  );
}

/* ============================================================
   工具
   ============================================================ */

function hhmm(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(+d)) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

function elapsedMinutes(g: GoalView) {
  const start = new Date(g.createdAt).getTime();
  const end =
    g.status === 'done' && g.completedAt ? new Date(g.completedAt).getTime() : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end)) return 0;
  return Math.max(1, Math.round((end - start) / 60000));
}

const s = StyleSheet.create({
  page: {
    paddingHorizontal: PAGE_X,
    paddingTop: gap.xs,
    paddingBottom: 110,
  },
});
