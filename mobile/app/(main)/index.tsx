import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import { useFocusEffect, useRouter } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Modal,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Avatar, AvatarStack, initialOf } from '../../src/components/Avatar';
import { Canvas } from '../../src/components/Canvas';
import { PressableScale } from '../../src/components/motion';
import { Pager } from '../../src/components/Pager';
import { Surface } from '../../src/components/Surface';
import {
  Divider,
  Icon,
  Row,
  Segmented,
  T,
  Tag,
  TextButton,
  grad,
  withAlpha,
  type IoniconName,
} from '../../src/components/ui';
import { normalizeActivity } from '../../src/lib/activity';
import { del, get, humanError, patch, post } from '../../src/lib/api';
import { getItem, setItem } from '../../src/lib/storage';
import { useApp } from '../../src/lib/store';
import type {
  Artifact,
  Connector,
  ConnectorsResponse,
  Conversation,
  Device,
  Goal,
  Message,
} from '../../src/lib/types';
import { isDeviceOnline, paperNo } from '../../src/lib/types';
import { useTheme } from '../../src/theme/theme';
import { MEMBER_COLORS, type as ts } from '../../src/theme/tokens';

/**
 * 主页：三页横滑（附录 B4）。
 *
 *   0 团队 —— Bento（A4）：第一个团队占整行做主视觉，其余两列小卡
 *   1 对话 —— 极简版（B3）：问候 + 一条待审批 + 最近对话 + 新建团队
 *   2 我   —— 深色英雄卡（A2）+ 额度卡 + 图标砖菜单（A5）
 *
 * 规范对应：
 *   §6.10  顶部悬浮玻璃胶囊分段，选中墨黑底白字，与 Pager 滑动同步
 *   A1     品牌渐变只用于：主按钮 / 进度填充 / 选中态 / 英雄数字
 *   A2     英雄卡底 t.heroBg + 两团低透明度蓝紫光晕，圆角 28
 *   A3     英文微标签 10.5/700 字距 2.2 + 27/750 大标题
 *   A4     Bento：禁止等高划一
 *   A5     44px 圆角 13 双色渐变砖 + 顶部内高光
 *   B1     没有 currentConv 时补拉 /v1/official 落到官方群
 *   B3     首页不放战况数字，统计收进「我」
 *   B4     顺序 团队 | 对话 | 我，默认停在对话
 *
 * 群聊本身不在这里——最近对话点进 /team/[id]，那里才是 §6.1-6.6 的群聊页。
 * 门面页允许品牌渐变，但只限主按钮 / 进度填充 / 选中态 / 英雄数字，绝不铺背景。
 */

const PAGE_CHAT = 1;
const PAGES = ['团队', '对话', '我'];

const OFFICIAL_STUDIO = 'official';
const OFFICIAL_GLYPH = '团';

/** 团队卡的状态标按语义取色：干活中墨黑、等你审批红、已完成绿。 */
type TeamTone = 'working' | 'blocked' | 'done' | 'idle';

export default function Main() {
  const t = useTheme();
  const router = useRouter();
  const user = useApp((s) => s.user);
  const convs = useApp((s) => s.convs);
  const setUser = useApp((s) => s.setUser);
  const setConvs = useApp((s) => s.setConvs);
  const setCurrentConv = useApp((s) => s.setCurrentConv);
  const setOfficial = useApp((s) => s.setOfficial);

  const [page, setPage] = useState(PAGE_CHAT); // B4：默认停在对话

  /* 全局统计：只给「我」页用，首页一个数字都不放（B3） */
  const [goals, setGoals] = useState<Goal[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [connectors, setConnectors] = useState<Connector[]>([]);
  const [devices, setDevices] = useState<Device[]>([]);
  /** 官方群最近一条消息 + 各群最近消息，喂给「最近对话」列表 */
  const [recent, setRecent] = useState<Record<string, Message>>({});
  /** 每个群的状态标（团队卡）+ 待审批件数（首页唯一允许的提醒） */
  const [teamStates, setTeamStates] = useState<Record<string, TeamTone>>({});
  const [blockedCount, setBlockedCount] = useState<Record<string, number>>({});
  const [pinned, setPinned] = useState<string[]>([]);
  const [err, setErr] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [menuFor, setMenuFor] = useState<Conversation | null>(null);

  const convsRef = useRef(convs);
  convsRef.current = convs;

  /* 置顶团队：本地持久化即可（不属于服务端数据） */
  useEffect(() => {
    (async () => {
      const raw = await getItem('zhituan_pinned_teams');
      if (!raw) return;
      try {
        const v = JSON.parse(raw);
        if (Array.isArray(v)) setPinned(v.filter((x) => typeof x === 'string'));
      } catch {
        /* 脏数据忽略 */
      }
    })();
  }, []);

  const togglePin = useCallback((id: string) => {
    setPinned((prev) => {
      const next = prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id];
      void setItem('zhituan_pinned_teams', JSON.stringify(next));
      return next;
    });
  }, []);

  /* ---------- 首次进入落到官方群（B1） ---------- */
  useEffect(() => {
    let alive = true;
    (async () => {
      if (useApp.getState().currentConv) return;
      try {
        const off = await get<{ conversation: Conversation; hints: string[] }>('/v1/official');
        if (!alive || !off?.conversation?.id) return;
        setOfficial(off.conversation.id, off.hints ?? []);
        setCurrentConv(off.conversation.id);
        const list = await get<Conversation[]>('/v1/conversations');
        if (alive) setConvs(list ?? []);
      } catch (e) {
        if (alive) setErr(humanError(e));
      }
    })();
    return () => {
      alive = false;
    };
  }, [setOfficial, setCurrentConv, setConvs]);

  /* ---------- 数据 ---------- */
  const loadStats = useCallback(async () => {
    const [g, a, c, d] = await Promise.allSettled([
      get<Goal[]>('/v1/goals'),
      get<Artifact[]>('/v1/library'),
      get<ConnectorsResponse>('/v1/connectors'),
      get<Device[]>('/v1/devices'),
    ]);
    if (g.status === 'fulfilled') setGoals(Array.isArray(g.value) ? g.value : []);
    if (a.status === 'fulfilled') setArtifacts(Array.isArray(a.value) ? a.value : []);
    if (c.status === 'fulfilled') setConnectors(c.value?.items ?? []);
    if (d.status === 'fulfilled') setDevices(Array.isArray(d.value) ? d.value : []);
  }, []);

  /** 每个群拉最近一条消息 + 状态环。群不多（通常 < 10），可以并发。 */
  const loadRecent = useCallback(async (list: Conversation[]) => {
    const next: Record<string, Message> = {};
    const states: Record<string, TeamTone> = {};
    const blocked: Record<string, number> = {};
    await Promise.all(
      list.map(async (c) => {
        const [m, act] = await Promise.allSettled([
          get<{ messages?: Message[] } | Message[]>(`/v1/conversations/${c.id}/messages?limit=1`),
          get<unknown>(`/v1/conversations/${c.id}/activity`),
        ]);
        if (m.status === 'fulfilled') {
          const arr = Array.isArray(m.value) ? m.value : (m.value?.messages ?? []);
          const last = arr[arr.length - 1];
          if (last) next[c.id] = last;
        }
        if (act.status === 'fulfilled') {
          const acts = normalizeActivity(act.value);
          states[c.id] = toneOfActivity(acts);
          const n = acts.filter((a) => a.state === 'blocked').length;
          if (n > 0) blocked[c.id] = n;
        }
      }),
    );
    setRecent(next);
    setTeamStates(states);
    setBlockedCount(blocked);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [cv, me] = await Promise.allSettled([
        get<Conversation[]>('/v1/conversations'),
        get<any>('/v1/me'),
      ]);
      if (cv.status === 'fulfilled') setConvs(cv.value ?? []);
      if (me.status === 'fulfilled') setUser(me.value?.user ?? me.value);
      const list = cv.status === 'fulfilled' ? (cv.value ?? []) : convsRef.current;
      await Promise.all([loadStats(), loadRecent(list)]);
    } catch (e) {
      setErr(humanError(e));
    }
  }, [setConvs, setUser, loadStats, loadRecent]);

  useEffect(() => {
    loadStats();
  }, [loadStats]);

  useEffect(() => {
    if (convs.length) loadRecent(convs);
  }, [convs, loadRecent]);

  // 从 /team/[id] 返回时刷新：删群、改名、审核完之后门面要跟上
  useFocusEffect(
    useCallback(() => {
      loadStats();
      if (convsRef.current.length) loadRecent(convsRef.current);
    }, [loadStats, loadRecent]),
  );

  /* ---------- 派生 ---------- */
  const officialConv = convs.find((c) => c.studio === OFFICIAL_STUDIO) ?? null;
  /** 官方群是系统群，不进团队页、不占团队名额（B1） */
  const teamConvs = useMemo(() => convs.filter((c) => c.studio !== OFFICIAL_STUDIO), [convs]);

  /** 「最近对话」排序：官方群常驻第一位（B1），其余按最后消息时间倒序 */
  const chatList = useMemo(() => {
    const rest = convs.filter((c) => c.studio !== OFFICIAL_STUDIO);
    rest.sort((a, b) => stampOf(recent[b.id], b) - stampOf(recent[a.id], a));
    return officialConv ? [officialConv, ...rest] : rest;
  }, [convs, recent, officialConv]);

  /** 唯一允许的统计类提示（B3）：哪支团队在等你审批 */
  const pending = useMemo(() => {
    const hit = Object.entries(blockedCount)
      .filter(([id]) => convs.some((c) => c.id === id))
      .sort((a, b) => b[1] - a[1])[0];
    if (!hit) return null;
    const c = convs.find((x) => x.id === hit[0]);
    return c ? { conv: c, count: hit[1] } : null;
  }, [blockedCount, convs]);

  const doingCount = goals.filter((g) => g.status === 'doing').length;
  /** 团队页副标题：本周完成的目标数（统计只出现在团队/我，不在对话页） */
  const doneThisWeek = useMemo(() => {
    const now = new Date();
    const weekStart = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate() - ((now.getDay() + 6) % 7),
    );
    return goals.filter((g) => {
      if (g.status !== 'done') return false;
      const at = (g as Goal & { completedAt?: string }).completedAt;
      if (!at) return false;
      const d = new Date(at);
      return !Number.isNaN(d.getTime()) && d >= weekStart;
    }).length;
  }, [goals]);
  const connected = connectors.find((c) => c.enabled);

  /* ---------- 团队操作 ---------- */
  function openTeam(id: string) {
    setCurrentConv(id);
    router.push(`/team/${id}`);
  }

  async function copyTeam(c: Conversation) {
    setMenuFor(null);
    try {
      // B4：复制要连成员人设与权限级别一起复制
      const conv = await post<Conversation>('/v1/conversations', {
        title: `${c.title} 副本`,
        studio: c.studio,
        agents: c.agents ?? [],
        members: c.members ?? [],
        defaultPermission: c.defaultPermission || undefined,
        agentPermissions: c.agentPermissions ?? undefined,
      });
      let list: Conversation[] | null = null;
      try {
        list = await get<Conversation[]>('/v1/conversations');
      } catch {
        /* 列表刷新失败不影响复制本身 */
      }
      setConvs(list ?? (conv?.id ? [conv, ...convsRef.current] : convsRef.current));
    } catch (e) {
      setErr(humanError(e));
    }
  }

  function removeTeam(c: Conversation) {
    setMenuFor(null);
    Alert.alert('删除团队', `「${c.title}」将被解散，群里的记录不再可见。`, [
      { text: '取消', style: 'cancel' },
      {
        text: '删除',
        style: 'destructive',
        onPress: async () => {
          try {
            await del(`/v1/conversations/${c.id}`);
            setConvs(convs.filter((x) => x.id !== c.id));
          } catch (e) {
            setErr(humanError(e));
          }
        },
      },
    ]);
  }

  async function setPermission(c: Conversation) {
    setMenuFor(null);
    // 三级循环切换：需要审批 → 工作区内修改 → 完全访问权限
    const order = ['ask', 'approval', 'full'];
    const next = order[(order.indexOf(c.defaultPermission ?? 'approval') + 1) % order.length];
    try {
      await patch(`/v1/conversations/${c.id}`, { defaultPermission: next });
      setConvs(convs.map((x) => (x.id === c.id ? { ...x, defaultPermission: next } : x)));
    } catch (e) {
      setErr(humanError(e));
    }
  }

  /* ---------- 布局 ---------- */
  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        {/* §6.10：顶部悬浮玻璃胶囊，悬浮在内容之上，不贴边 */}
        <View style={s.navFloat}>
          <Segmented items={PAGES} index={page} onChange={setPage} />
        </View>

        <Pager index={page} onIndexChange={setPage}>
          <TeamsPage
            convs={teamConvs}
            states={teamStates}
            goals={goals}
            doneThisWeek={doneThisWeek}
            pinned={pinned}
            officialConv={officialConv}
            onOpen={openTeam}
            onNew={() => router.push('/team/new')}
            onMore={setMenuFor}
            onEdit={setMenuFor}
            onCopy={copyTeam}
            refreshing={refreshing}
            onRefresh={async () => {
              setRefreshing(true);
              await refresh();
              setRefreshing(false);
            }}
          />

          <ChatPage
            convs={chatList}
            recent={recent}
            states={teamStates}
            pending={pending}
            onOpen={openTeam}
            onNew={() => router.push('/team/new')}
          />

          <MePage
            user={user}
            convs={teamConvs}
            goals={goals}
            doing={doingCount}
            artifacts={artifacts}
            connectors={connectors}
            connected={connected}
            devices={devices}
            onOpenGoals={() => router.push('/goals')}
            onOpenLibrary={() => router.push('/library')}
            onOpenConnect={() => router.push('/connect')}
            onOpenDevices={() => router.push('/settings')}
            onOpenSettings={() => router.push('/settings')}
          />
        </Pager>

        {err ? (
          <Pressable onPress={() => setErr('')} style={[s.banner, { backgroundColor: t.badSoft }]}>
            <Icon name="alert-circle" size={15} color={t.bad} />
            <T size={ts.sub} color={t.bad} style={{ flex: 1 }} numberOfLines={2}>
              {err}
            </T>
          </Pressable>
        ) : null}

        <TeamMenu
          conv={menuFor}
          onClose={() => setMenuFor(null)}
          pinned={pinned}
          onEdit={(c) => {
            // 改名 / 公告 / 成员都在群信息面板里——直接进群
            setMenuFor(null);
            openTeam(c.id);
          }}
          onCopy={copyTeam}
          onMembers={(c) => {
            setMenuFor(null);
            openTeam(c.id);
          }}
          onPermission={setPermission}
          onPin={(c) => {
            togglePin(c.id);
            setMenuFor(null);
          }}
          onDelete={removeTeam}
        />
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   页 1：团队（左）
   ============================================================ */

function TeamsPage({
  convs,
  states,
  goals,
  doneThisWeek,
  pinned,
  officialConv,
  onOpen,
  onNew,
  onMore,
  onEdit,
  onCopy,
  refreshing,
  onRefresh,
}: {
  convs: Conversation[];
  states: Record<string, TeamTone>;
  goals: Goal[];
  doneThisWeek: number;
  pinned: string[];
  officialConv: Conversation | null;
  onOpen: (id: string) => void;
  onNew: () => void;
  onMore: (c: Conversation) => void;
  onEdit: (c: Conversation) => void;
  onCopy: (c: Conversation) => void;
  refreshing: boolean;
  onRefresh: () => void;
}) {
  const t = useTheme();

  // Bento 的主角永远是「最该被看到的那一个」：有审批的优先，
  // 其次是官方群（首次进入的落点），再其次才是正在干活的。
  // 置顶改变的是顺序，不改变卡型——第一张永远占整行。
  const ranked = useMemo(() => {
    const score = (c: Conversation) => {
      if (states[c.id] === 'blocked') return 3;
      if (c.id === officialConv?.id) return 2;
      if (states[c.id] === 'working') return 1;
      return 0;
    };
    return [...convs].sort((a, b) => {
      const pin = pinned.indexOf(a.id);
      const pin2 = pinned.indexOf(b.id);
      if (pin >= 0 || pin2 >= 0) {
        if (pin < 0) return 1;
        if (pin2 < 0) return -1;
        return pin - pin2;
      }
      return score(b) - score(a);
    });
  }, [convs, states, officialConv, pinned]);

  // 「一主卡 + 两列小卡」只在卡够多时才成立：
  // 只有两个团队时，主卡占满一行、第二张孤零零卡在左半边，看着像断版。
  // 两张就并排等宽，三张以上才让第一张当主视觉。
  const useHero = ranked.length >= 3;
  const hero = useHero ? ranked[0] : null;
  const others = useHero ? ranked.slice(1) : ranked;
  /** 目标进度：优先用这个群在做的目标，没有就退到全局第一个进行中的 */
  const goalFor = (c: Conversation): Goal | undefined =>
    goals.find((g) => g.conversationId === c.id && g.status === 'doing') ??
    goals.find((g) => g.status === 'doing') ??
    goals.find((g) => g.conversationId === c.id);

  return (
    <ScrollView
      style={{ flex: 1 }}
      contentContainerStyle={s.page}
      showsVerticalScrollIndicator={false}
      refreshControl={
        <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={t.accent} />
      }
    >
      {/* A3：英文微标签塞进大标题上方 */}
      <View style={s.titleRow}>
        <View style={{ flex: 1 }}>
          <T size={10.5} weight="700" color={t.ink3} tracking={2.2} style={s.eyebrow}>
            MY TEAMS
          </T>
          <T size={ts.display} weight="750" lh={34} tracking={-0.6} style={{ marginTop: 6 }}>
            团队
          </T>
          <T size={ts.sub} color={t.ink3} style={{ marginTop: 6 }}>
            {convs.length} 个团队 · 本周共完成 {doneThisWeek} 个目标
          </T>
        </View>
        {/* 品牌渐变只做「主操作」这一件事 */}
        <BrandPlusFab onPress={onNew} />
      </View>

      {convs.length === 0 ? (
        <View style={[s.emptyCard, { backgroundColor: t.card, borderColor: t.line }]}>
          <T size={ts.title} weight="650">
            还没有团队
          </T>
          <T size={ts.caption} color={t.ink3} align="center" style={{ marginTop: 4 }}>
            从模板开始，一句话就能派活
          </T>
          <TextButton label="新建团队" onPress={onNew} fontWeight="600" />
        </View>
      ) : (
        <>
          {/* A4：主视觉占整行 */}
          {hero ? (
            <HeroTeamCard
              conv={hero}
              tone={states[hero.id] ?? 'idle'}
              goal={goalFor(hero)}
              onOpen={() => onOpen(hero.id)}
              onEdit={() => onEdit(hero)}
              onCopy={() => onCopy(hero)}
              onMore={() => onMore(hero)}
            />
          ) : null}

          {/* 其余两列小卡 */}
          <View style={s.bentoRow}>
            {others.map((c) => (
              <View key={c.id} style={s.bentoCell}>
                <SmallTeamCard
                  conv={c}
                  tone={states[c.id] ?? 'idle'}
                  onOpen={() => onOpen(c.id)}
                  onEdit={() => onEdit(c)}
                  onCopy={() => onCopy(c)}
                  onMore={() => onMore(c)}
                />
              </View>
            ))}
            {others.length % 2 === 1 ? <View style={s.bentoCell} /> : null}
          </View>
        </>
      )}

      {convs.length ? (
        <T
          size={ts.caption}
          color={t.ink4}
          align="center"
          lh={19}
          style={{ marginTop: 26, paddingHorizontal: 12 }}
        >
          「复制」= 连成员人设与权限级别一起复制，改个名就是一支新队
        </T>
      ) : null}
    </ScrollView>
  );
}

/** 右上角圆形品牌渐变「+」。 */
function BrandPlusFab({ onPress }: { onPress: () => void }) {
  const t = useTheme();
  return (
    <PressableScale onPress={onPress} scaleTo={0.92} accessibilityLabel="新建团队">
      <LinearGradient
        colors={grad(t.gradient)}
        start={{ x: 0, y: 0 }}
        end={{ x: 1, y: 1 }}
        style={[s.fab, { shadowColor: t.brandShadow }]}
      >
        <Icon name="add" size={24} color="#FFFFFF" />
      </LinearGradient>
    </PressableScale>
  );
}

const TONE_META: Record<TeamTone, { label: string; tone: 'blocked' | 'done' | 'neutral' }> = {
  blocked: { label: '等你审批', tone: 'blocked' },
  working: { label: '干活中', tone: 'neutral' },
  done: { label: '刚完成', tone: 'done' },
  // 设计图 13 里「进行中」是绿色胶囊；语义色只在这里和「刚完成」共用
  idle: { label: '进行中', tone: 'done' },
};

/** 整行团队卡：成员头像组 + 团队名 + 状态标 + 目标进度 + 操作行。 */
function HeroTeamCard({
  conv,
  tone,
  goal,
  onOpen,
  onEdit,
  onCopy,
  onMore,
}: {
  conv: Conversation;
  tone: TeamTone;
  goal?: Goal;
  onOpen: () => void;
  onEdit: () => void;
  onCopy: () => void;
  onMore: () => void;
}) {
  const t = useTheme();
  const names = memberNames(conv);
  const meta = TONE_META[tone];
  // 目标进度：后端带了步骤清单就显示「3 / 5 步」，否则退回百分比
  const steps = (goal as (Goal & { steps?: unknown[] }) | undefined)?.steps;
  const totalSteps = Array.isArray(steps) ? steps.length : 0;
  const doneSteps = Array.isArray(steps)
    ? steps.filter((x) => (x as { status?: string })?.status === 'done').length
    : 0;
  const pct = goal ? Math.round(Math.max(0, Math.min(100, goal.progress))) : 0;
  const label = goal ? goal.title : '还没有目标';
  const progressText = totalSteps ? `目标 ${doneSteps} / ${totalSteps}` : `目标进度 ${pct}%`;

  return (
    <Surface outerRadius={26} style={{ marginTop: 20 }}>
      <Pressable
        onPress={onOpen}
        style={{ paddingTop: 16, paddingHorizontal: 16 }}
        accessibilityRole="button"
      >
        <Row gap={12} align="flex-start">
          <View style={{ flexDirection: 'row' }}>
            <MemberStack
              names={names.slice(0, 4)}
              tints={memberTints(conv).slice(0, 4)}
              size={40}
            />
          </View>
          <View style={{ flex: 1, marginLeft: 2 }}>
            <T size={ts.title} weight="750" numberOfLines={1}>
              {conv.title}
            </T>
            <T size={ts.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 3 }}>
              {names.length} 位成员 · {meta.label}
            </T>
          </View>
          <Tag label={meta.label} tone={meta.tone} />
        </Row>

        <View style={{ marginTop: 16 }}>
          <Row style={{ justifyContent: 'space-between', marginBottom: 7 }}>
            <T size={ts.caption} color={t.ink2} weight="600" numberOfLines={1} style={{ flex: 1 }}>
              {progressText}
            </T>
            <T
              size={ts.caption}
              color={t.ink3}
              weight="600"
              numberOfLines={1}
              style={{ maxWidth: 118 }}
            >
              {label}
            </T>
          </Row>
          {/* A1：进度填充可以用品牌渐变 */}
          <View style={[s.track, { backgroundColor: t.track }]}>
            <LinearGradient
              colors={grad(t.gradient)}
              start={{ x: 0, y: 0 }}
              end={{ x: 1, y: 0 }}
              style={{
                width: `${totalSteps ? (doneSteps / totalSteps) * 100 : pct}%`,
                height: 5,
                borderRadius: 99,
              }}
            />
          </View>
        </View>
      </Pressable>

      <View style={[s.heroActions, { borderTopColor: t.line }]}>
        <PressableScale onPress={onOpen} scaleTo={0.96}>
          <Row gap={6}>
            <Icon name="chatbubble-outline" size={15} color={t.ink} />
            <T size={ts.sub} weight="600">
              进入群聊
            </T>
          </Row>
        </PressableScale>
        <Row gap={22}>
          <TextButton label="编辑" onPress={onEdit} size={ts.caption} />
          <TextButton label="复制" onPress={onCopy} size={ts.caption} />
          <Pressable onPress={onMore} hitSlop={10} accessibilityLabel="更多操作">
            <Icon name="ellipsis-horizontal" size={18} color={t.ink3} />
          </Pressable>
        </Row>
      </View>
    </Surface>
  );
}

/** 两列小卡：只保留编辑 / 复制。 */
function SmallTeamCard({
  conv,
  tone,
  onOpen,
  onEdit,
  onCopy,
  onMore,
}: {
  conv: Conversation;
  tone: TeamTone;
  onOpen: () => void;
  onEdit: () => void;
  onCopy: () => void;
  onMore: () => void;
}) {
  const t = useTheme();
  const names = memberNames(conv);
  const meta = TONE_META[tone];
  const tint = memberTints(conv)[0] ?? MEMBER_COLORS[0];

  return (
    <Surface outerRadius={26} style={{ flex: 1 }}>
      <Pressable onPress={onOpen} style={{ padding: 15 }} accessibilityRole="button">
        <Row style={{ justifyContent: 'space-between' }}>
          <InitialTile name={conv.title} tint={tint} size={40} />
          <Pressable onPress={onMore} hitSlop={12} accessibilityLabel="更多操作">
            <Icon name="ellipsis-horizontal" size={16} color={t.ink4} />
          </Pressable>
        </Row>
        <T size={ts.body} weight="700" numberOfLines={1} style={{ marginTop: 12 }}>
          {conv.title}
        </T>
        <T size={ts.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 3 }}>
          {names.length} 位成员 · {meta.label}
        </T>
      </Pressable>
      <View style={[s.smallActions, { borderTopColor: t.line }]}>
        <TextButton label="编辑" onPress={onEdit} size={ts.micro} />
        <TextButton label="复制" onPress={onCopy} size={ts.micro} />
      </View>
    </Surface>
  );
}

/** 团队图标砖：首字落在低饱和身份色上（A5 的静默版，不抢英雄卡）。 */
function InitialTile({ name, tint, size = 40 }: { name: string; tint: string; size?: number }) {
  return (
    <LinearGradient
      colors={grad([withAlpha(tint, 0.85), tint])}
      start={{ x: 0, y: 0 }}
      end={{ x: 1, y: 1 }}
      style={{
        width: size,
        height: size,
        borderRadius: 13,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <View pointerEvents="none" style={s.tileHighlight} />
      <T size={size * 0.4} weight="700" color="#FFFFFF">
        {initialOf(name)}
      </T>
    </LinearGradient>
  );
}

/** 头像重叠组。与 AvatarStack 的区别：尺寸更大、环色跟卡片底一致。 */
function MemberStack({ names, tints, size }: { names: string[]; tints: string[]; size: number }) {
  return <AvatarStack names={names} tints={tints} size={size} max={4} />;
}

/* ============================================================
   页 2：对话（中，极简版 · 附录 B3）
   ============================================================ */

function ChatPage({
  convs,
  recent,
  states,
  pending,
  onOpen,
  onNew,
}: {
  convs: Conversation[];
  recent: Record<string, Message>;
  states: Record<string, TeamTone>;
  pending: { conv: Conversation; count: number } | null;
  onOpen: (id: string) => void;
  onNew: () => void;
}) {
  const t = useTheme();
  const now = new Date();
  const hello = now.getHours() < 12 ? '早上好' : now.getHours() < 18 ? '下午好' : '晚上好';

  return (
    <View style={{ flex: 1 }}>
      <ScrollView
        contentContainerStyle={[s.page, { paddingBottom: 150 }]}
        showsVerticalScrollIndicator={false}
      >
        <T
          size={10.5}
          weight="700"
          color={t.ink3}
          tracking={2.2}
          style={[s.eyebrow, { marginTop: 22 }]}
        >
          {`TODAY · ${now.getMonth() + 1} 月 ${now.getDate()} 日`}
        </T>
        <T size={ts.display} weight="750" lh={34} tracking={-0.6} style={{ marginTop: 7 }}>
          {hello}，纸团
        </T>
        <T size={ts.sub} color={t.ink3} style={{ marginTop: 7 }}>
          你的团队正在替你干活。
        </T>

        {/* 唯一允许的统计类信息：一条待审批提醒 */}
        {pending ? (
          <Surface outerRadius={22} style={{ marginTop: 20 }}>
            <Pressable
              onPress={() => onOpen(pending.conv.id)}
              style={s.approvalRow}
              accessibilityRole="button"
            >
              <View style={[s.warnDot, { backgroundColor: t.warn }]} />
              <T size={ts.sub} weight="650" numberOfLines={1} style={{ flex: 1 }}>
                {pending.conv.title} · 等你审批 {pending.count} 件
              </T>
              <Row gap={2}>
                <T size={ts.caption} color={t.accentText} weight="650">
                  去处理
                </T>
                <Icon name="chevron-forward" size={13} color={t.accentText} />
              </Row>
            </Pressable>
          </Surface>
        ) : null}

        <Row
          style={{
            justifyContent: 'space-between',
            marginTop: 24,
            marginBottom: 10,
          }}
        >
          <T size={ts.caption} color={t.ink3} weight="650">
            最近对话
          </T>
          <T size={10} color={t.ink4} weight="700" tracking={1.4}>
            RECENT
          </T>
        </Row>

        {convs.length === 0 ? (
          <View style={[s.emptyCard, { backgroundColor: t.card, borderColor: t.line }]}>
            <T size={ts.title} weight="650">
              还没有对话
            </T>
            <T size={ts.caption} color={t.ink3} style={{ marginTop: 4 }}>
              建一个团队，或者去新手村看看
            </T>
          </View>
        ) : (
          <Surface outerRadius={24}>
            {convs.map((c, i) => (
              <View key={c.id}>
                {i > 0 ? <Divider inset={72} /> : null}
                <ConversationRow
                  conv={c}
                  msg={recent[c.id]}
                  tone={states[c.id] ?? 'idle'}
                  onPress={() => onOpen(c.id)}
                />
              </View>
            ))}
          </Surface>
        )}
      </ScrollView>

      {/* 底部：品牌渐变主按钮 + 从模板开始（B3 的最后两样） */}
      <View style={[s.chatFooter, { backgroundColor: t.canvas[1] }]}>
        <PressableScale onPress={onNew} scaleTo={0.97} accessibilityLabel="新建团队">
          <LinearGradient
            colors={grad(t.gradient)}
            start={{ x: 0, y: 0 }}
            end={{ x: 1, y: 1 }}
            style={[s.primaryBtn, { shadowColor: t.brandShadow }]}
          >
            <Icon name="add" size={18} color="#FFFFFF" />
            <T size={ts.body} weight="700" color="#FFFFFF">
              新建团队
            </T>
          </LinearGradient>
        </PressableScale>
        <Pressable onPress={onNew} hitSlop={10} style={{ marginTop: 10 }}>
          <T size={ts.caption} color={t.ink4}>
            从模板开始 ›
          </T>
        </Pressable>
      </View>
    </View>
  );
}

function ConversationRow({
  conv,
  msg,
  tone,
  onPress,
}: {
  conv: Conversation;
  msg?: Message;
  tone: TeamTone;
  onPress: () => void;
}) {
  const t = useTheme();
  const official = conv.studio === OFFICIAL_STUDIO;
  const names = memberNames(conv);
  const tints = memberTints(conv);
  const preview = msg ? summarize(msg) : '还没有消息';
  const time = msg ? relativeTime(msg.createdAt) : '';

  return (
    <Pressable onPress={onPress} style={s.convRow} accessibilityRole="button">
      {official ? (
        <OfficialAvatar size={46} />
      ) : names.length > 1 ? (
        <MiniStack names={names.slice(0, 4)} tints={tints.slice(0, 4)} />
      ) : (
        <InitialTile name={conv.title} tint={tints[0] ?? MEMBER_COLORS[0]} size={46} />
      )}

      <View style={{ flex: 1 }}>
        <Row gap={6}>
          <T size={ts.body} weight="700" numberOfLines={1} style={{ flexShrink: 1 }}>
            {conv.title}
          </T>
          {official ? <MiniTag label="官方" fg={t.goldFg} bg={t.goldBg} /> : null}
          {tone === 'blocked' ? <MiniTag label="等你审批" fg={t.warn} bg={t.warnSoft} /> : null}
        </Row>
        <T size={ts.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 3 }}>
          {preview}
        </T>
      </View>

      <View style={{ alignItems: 'flex-end', gap: 7, minWidth: 44 }}>
        <T size={ts.micro} color={t.ink4}>
          {time}
        </T>
        {official ? (
          <View style={[s.unread, { backgroundColor: t.accent }]} />
        ) : tone === 'blocked' ? (
          <View style={[s.unread, { backgroundColor: t.dangerDot }]} />
        ) : (
          <View style={{ width: 8, height: 8 }} />
        )}
      </View>
    </Pressable>
  );
}

/** 官方群头像：与设计图 v19 一致的官方蓝砖 + 白字（身份色，不是渐变）。 */
function OfficialAvatar({ size = 46 }: { size?: number }) {
  const t = useTheme();
  return (
    <LinearGradient
      colors={grad([lightenHex(t.meBlue, 0.16), t.meBlue])}
      start={{ x: 0, y: 0 }}
      end={{ x: 1, y: 1 }}
      style={{
        width: size,
        height: size,
        borderRadius: 14,
        alignItems: 'center',
        justifyContent: 'center',
        overflow: 'hidden',
      }}
    >
      <View pointerEvents="none" style={s.tileHighlight} />
      <T size={size * 0.42} weight="750" color="#FFFFFF">
        {OFFICIAL_GLYPH}
      </T>
    </LinearGradient>
  );
}

/**
 * 会话行左侧的 2×2 头像组（设计图里团队头像就是这种拼贴）。
 *
 * 位置只有 46px，单排叠放塞不下 3 个以上头像——试过会溢出、
 * 计数被挤成竖排。所以保持 2×2，第四个位置在人多时显示 +N。
 */
function MiniStack({ names, tints }: { names: string[]; tints: string[] }) {
  const t = useTheme();
  // 超过 4 人时右下角那格换成 +N：四个人刚好填满 2×2，
  // 再加一格会溢出容器，把第五个人直接省略又等于没说。
  const overflow = names.length > 4;
  const shown = overflow ? names.slice(0, 3) : names.slice(0, 4);
  const cell = (n: string, tint: string, i: number) => (
    <View key={n + i} style={{ marginLeft: i === 0 ? 0 : -6 }}>
      <Avatar name={n} tint={tint} size={24} />
    </View>
  );
  return (
    <View style={[s.miniStack, { backgroundColor: t.cardSunken }]}>
      <View style={{ flexDirection: 'row' }}>
        {shown.slice(0, 2).map((n, i) => cell(n, tints[i], i))}
      </View>
      <View style={{ flexDirection: 'row', marginTop: 2 }}>
        {shown.slice(2, 4).map((n, i) => cell(n, tints[i + 2], i))}
        {overflow ? (
          <View
            style={{
              marginLeft: -6,
              width: 24,
              height: 24,
              borderRadius: 8,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: t.track,
            }}
          >
            <T size={9} weight="700" color={t.ink3}>
              +{names.length - 3}
            </T>
          </View>
        ) : null}
      </View>
    </View>
  );
}

function MiniTag({ label, fg, bg }: { label: string; fg: string; bg: string }) {
  return (
    <View
      style={{
        paddingHorizontal: 6,
        paddingVertical: 2,
        borderRadius: 5,
        backgroundColor: bg,
      }}
    >
      <T size={10} weight="700" color={fg}>
        {label}
      </T>
    </View>
  );
}

/* ============================================================
   页 3：我（右，§9 第 7 屏 + 附录 A2）
   ============================================================ */

function MePage({
  user,
  convs,
  goals,
  doing,
  artifacts,
  connectors,
  connected,
  devices,
  onOpenGoals,
  onOpenLibrary,
  onOpenConnect,
  onOpenDevices,
  onOpenSettings,
}: {
  user: any;
  convs: Conversation[];
  goals: Goal[];
  doing: number;
  artifacts: Artifact[];
  connectors: Connector[];
  connected?: Connector;
  devices: Device[];
  onOpenGoals: () => void;
  onOpenLibrary: () => void;
  onOpenConnect: () => void;
  onOpenDevices: () => void;
  onOpenSettings: () => void;
}) {
  const t = useTheme();
  const quota = Math.max(1, user?.quota ?? 100);
  const used = Math.max(0, user?.used ?? 0);
  const remainPct = Math.max(0, Math.min(100, Math.round(((quota - used) / quota) * 100)));
  const myDevice = devices.find((d) => !d.revokedAt);
  // 在线必须看设备最后一次心跳，不能只看「有没有配过对」：
  // 后端每 10 分钟落一次 LastSeen，两次心跳间隔的 2.5 倍算离线，
  // 留够余量避免手机切后台回来时闪一下「离线」。
  const online = isDeviceOnline(myDevice);
  const prefLevel = (user as { preferences?: Record<string, string> } | null)?.preferences
    ?.defaultPermission;
  const defaultLevel = prefLevel ?? convs.find((c) => c.defaultPermission)?.defaultPermission;
  const reset = nextResetLabel();

  return (
    <ScrollView
      contentContainerStyle={[s.page, { paddingBottom: 40 }]}
      showsVerticalScrollIndicator={false}
    >
      {/* A2 深色英雄卡 */}
      <View
        style={[s.heroShadow, { shadowColor: t.shadowColor, shadowOpacity: t.dark ? 0.5 : 0.28 }]}
      >
        <View style={[s.hero, { backgroundColor: t.heroBg }]}>
          {/* 两团蓝紫径向光晕：半透明大圆 + 低透明度，溢出裁切 */}
          <View
            pointerEvents="none"
            style={[s.glow, s.glowA, { backgroundColor: withAlpha(t.accent, 0.55) }]}
          />
          <View
            pointerEvents="none"
            style={[s.glow, s.glowB, { backgroundColor: withAlpha(t.accentDeep, 0.45) }]}
          />

          <View style={s.heroInner}>
            <Row gap={13} align="center">
              <LinearGradient
                colors={grad([lightenHex(t.meBlue, 0.18), t.meBlue])}
                start={{ x: 0.2, y: 0 }}
                end={{ x: 0.8, y: 1 }}
                style={s.meAvatar}
              >
                <T size={22} weight="750" color="#FFFFFF">
                  {initialOf(user?.name ?? '我')}
                </T>
              </LinearGradient>
              <View style={{ flex: 1 }}>
                <T size={ts.page} weight="750" color="#FFFFFF" numberOfLines={1}>
                  {user?.name?.trim() || '纸团'}
                </T>
                <T
                  size={ts.caption}
                  color="rgba(255,255,255,0.65)"
                  numberOfLines={1}
                  style={{ marginTop: 3 }}
                >
                  纸团号 {paperNo(user?.id)} · {online ? '电脑在线' : '电脑离线'}
                </T>
              </View>
              <Pressable onPress={onOpenSettings} hitSlop={12} accessibilityLabel="账号设置">
                <Icon name="chevron-forward" size={16} color="rgba(255,255,255,0.45)" />
              </Pressable>
            </Row>

            {/* 三列统计：英雄卡上的数字 */}
            <Row style={{ marginTop: 20 }} align="flex-start">
              <HeroStat label="团队" value={String(convs.length)} />
              <HeroStat label="进行中目标" value={String(doing)} />
              <HeroStat label="资料库文件" value={String(artifacts.length)} />
            </Row>

            {/* 内嵌额度卡：rgba(255,255,255,.09) 底 + .10 描边 */}
            <View style={s.quotaCard}>
              <Row style={{ justifyContent: 'space-between' }}>
                <T size={ts.caption} color="rgba(255,255,255,0.55)">
                  本周额度
                </T>
                <T size={ts.caption} color="rgba(255,255,255,0.55)">
                  {reset} 重置
                </T>
              </Row>
              <Row gap={8} align="flex-end" style={{ marginTop: 6 }}>
                <T size={34} weight="750" color="#FFFFFF" lh={38}>
                  {remainPct}%
                </T>
                <T size={ts.caption} color="rgba(255,255,255,0.65)" style={{ marginBottom: 6 }}>
                  剩余可用
                </T>
              </Row>
              <View style={[s.quotaTrack, { backgroundColor: 'rgba(255,255,255,0.14)' }]}>
                <LinearGradient
                  colors={grad(t.gradient)}
                  start={{ x: 0, y: 0 }}
                  end={{ x: 1, y: 0 }}
                  style={{
                    width: `${remainPct}%`,
                    height: 6,
                    borderRadius: 99,
                  }}
                />
              </View>
            </View>
          </View>
        </View>
      </View>

      {/* A5 图标砖菜单 */}
      <Surface outerRadius={24} style={{ marginTop: 14 }}>
        <MenuRow
          icon="flag"
          colors={['#6A6AF2', '#4E7BFB']}
          label="我的目标"
          value={`进行中 ${doing} 个`}
          onPress={onOpenGoals}
        />
        <Divider inset={60} />
        <MenuRow
          icon="folder-open"
          colors={['#7C93A8', '#5F7E99']}
          label="资料库"
          value={`${artifacts.length} 个文件`}
          onPress={onOpenLibrary}
        />
        <Divider inset={60} />
        <MenuRow
          icon="git-network"
          colors={['#8B7EC8', '#6C5DAE']}
          label="连接器"
          value={connected ? `${connected.name} 已连接` : connectors.length ? '未连接' : '未设置'}
          onPress={onOpenConnect}
        />
        <Divider inset={60} />
        <MenuRow
          icon="laptop"
          colors={['#64766B', '#4C6154']}
          label="我的设备"
          value={online ? `${myDevice?.name ?? '这台电脑'} · 在线` : '未连接'}
          onPress={onOpenDevices}
        />
        <Divider inset={60} />
        <MenuRow
          icon="shield-checkmark"
          colors={['#B98A5E', '#9A6E45']}
          label="默认权限级别"
          value={levelLabel(defaultLevel)}
          // 权限级别在设置页里改；以前这里跳到设备页，点进去找不到这个设置。
          onPress={onOpenSettings}
        />
      </Surface>

      <Surface outerRadius={24} style={{ marginTop: 12 }}>
        <MenuRow
          icon="settings"
          colors={['#9AA0B4', '#6E7488']}
          label="设置"
          onPress={onOpenSettings}
          last
        />
      </Surface>

      <T size={ts.caption} color={t.ink4} align="center" style={{ marginTop: 24 }}>
        纸团 v1.1.1 · teamwise.cn
      </T>
    </ScrollView>
  );
}

function HeroStat({ label, value }: { label: string; value: string }) {
  return (
    <View style={{ flex: 1, alignItems: 'center' }}>
      {/* 深色卡上的数字用纯白：渐变字在 RN 里没法裁切文字，
          真正的渐变留给进度条和按钮（A1 的四种用法之一）。 */}
      <T size={28} weight="750" color="#FFFFFF" lh={32}>
        {value}
      </T>
      <T size={ts.caption} color="rgba(255,255,255,0.55)" style={{ marginTop: 2 }}>
        {label}
      </T>
    </View>
  );
}

/** 菜单行：44px 圆角 13 双色渐变砖 + 顶部内高光（A5）。 */
function MenuRow({
  icon,
  colors,
  label,
  value,
  onPress,
  last,
}: {
  icon: IoniconName;
  colors: [string, string];
  label: string;
  value?: string;
  onPress: () => void;
  last?: boolean;
}) {
  const t = useTheme();
  return (
    <PressableScale onPress={onPress} scaleTo={0.99}>
      <Row gap={14} style={[s.menuRow, last ? { paddingBottom: 14 } : null]}>
        <LinearGradient
          colors={grad(colors)}
          start={{ x: 0, y: 0 }}
          end={{ x: 1, y: 1 }}
          style={s.iconBrick}
        >
          <View pointerEvents="none" style={s.tileHighlight} />
          <Ionicons name={icon} size={20} color="#FFFFFF" />
        </LinearGradient>
        <T size={ts.body} weight="650" style={{ flex: 1 }} numberOfLines={1}>
          {label}
        </T>
        {value ? (
          <T size={ts.caption} color={t.ink4} numberOfLines={1}>
            {value}
          </T>
        ) : null}
        <Icon name="chevron-forward" size={14} color={t.ink4} />
      </Row>
    </PressableScale>
  );
}

/* ============================================================
   团队操作菜单（bottom sheet · 附录 C 第 5 屏）
   ============================================================ */

function TeamMenu({
  conv,
  pinned,
  onClose,
  onEdit,
  onCopy,
  onMembers,
  onPermission,
  onPin,
  onDelete,
}: {
  conv: Conversation | null;
  pinned: string[];
  onClose: () => void;
  onEdit: (c: Conversation) => void;
  onCopy: (c: Conversation) => void;
  onMembers: (c: Conversation) => void;
  onPermission: (c: Conversation) => void;
  onPin: (c: Conversation) => void;
  onDelete: (c: Conversation) => void;
}) {
  const t = useTheme();
  if (!conv) return null;
  const names = memberNames(conv);
  const tints = memberTints(conv);

  const items: {
    icon: IoniconName;
    label: string;
    value?: string;
    danger?: boolean;
    onPress: () => void;
  }[] = [
    {
      icon: 'pencil',
      label: '编辑团队',
      value: '改名、公告与成员',
      onPress: () => onEdit(conv),
    },
    {
      icon: 'copy-outline',
      label: '复制团队',
      value: '连人设与权限一起复制',
      onPress: () => onCopy(conv),
    },
    {
      icon: 'people-outline',
      label: '成员管理',
      onPress: () => onMembers(conv),
    },
    {
      icon: 'shield-checkmark-outline',
      label: '权限设置',
      value: `当前：${levelLabel(conv.defaultPermission)}`,
      onPress: () => onPermission(conv),
    },
    {
      icon: 'pin-outline',
      label: pinned.includes(conv.id) ? '取消置顶' : '置顶团队',
      onPress: () => onPin(conv),
    },
    {
      icon: 'trash-outline',
      label: '删除团队',
      danger: true,
      onPress: () => onDelete(conv),
    },
  ];

  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <Pressable style={[s.scrim, { backgroundColor: t.scrim }]} onPress={onClose}>
        <Pressable
          style={[s.sheet, { backgroundColor: t.card }]}
          onPress={(e) => e.stopPropagation()}
        >
          <View style={[s.grabber, { backgroundColor: t.line }]} />
          <Row gap={11} style={{ paddingHorizontal: 18, paddingBottom: 14 }}>
            <AvatarStack names={names.slice(0, 4)} tints={tints.slice(0, 4)} size={34} max={4} />
            <View style={{ flex: 1 }}>
              <T size={ts.title} weight="750" numberOfLines={1}>
                {conv.title}
              </T>
              <T size={ts.caption} color={t.ink3} style={{ marginTop: 2 }}>
                {names.length} 位成员 · 创建于 {monthDay(conv.createdAt)}
              </T>
            </View>
          </Row>

          {items.map((it, i) => (
            <View key={it.label}>
              {i === 0 ? <Divider /> : <Divider inset={18} />}
              <Pressable onPress={it.onPress} style={s.sheetRow} accessibilityRole="button">
                <Icon
                  name={it.icon}
                  size={19}
                  color={it.danger ? t.bad : t.ink}
                  style={{ width: 24 }}
                />
                <T
                  size={ts.body}
                  weight="600"
                  color={it.danger ? t.bad : t.ink}
                  style={{ flex: 1 }}
                >
                  {it.label}
                </T>
                {it.value ? (
                  <T size={ts.caption} color={it.danger ? t.bad : t.ink4} numberOfLines={1}>
                    {it.value}
                  </T>
                ) : null}
              </Pressable>
            </View>
          ))}

          <View style={{ padding: 14 }}>
            <Pressable onPress={onClose} style={[s.cancel, { backgroundColor: t.cardSunken }]}>
              <T size={ts.body} weight="650">
                取消
              </T>
            </Pressable>
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

/* ============================================================
   工具
   ============================================================ */

function memberNames(c: Conversation): string[] {
  if (c.agents?.length) return c.agents.map((a) => a.name);
  if (c.members?.length) return c.members;
  // 内置场景的群存在库里没有成员——后端列表接口会把解析结果
  // 放在 memberNames 里（见 handleListConvs）。
  const extra = c as Conversation & { memberNames?: string[] };
  return extra.memberNames ?? [];
}

function memberTints(c: Conversation): string[] {
  return memberNames(c).map(
    (_, i) => c.agents?.[i]?.color ?? MEMBER_COLORS[i % MEMBER_COLORS.length],
  );
}

/** 群状态：任何成员卡在审批 → blocked；有任何人在干活 → working。 */
function toneOfActivity(acts: { state: string }[]): TeamTone {
  if (acts.some((a) => a.state === 'blocked')) return 'blocked';
  if (acts.some((a) => a.state === 'working')) return 'working';
  if (acts.some((a) => a.state === 'done')) return 'done';
  return 'idle';
}

function levelLabel(level?: string): string {
  if (!level) return '跟随角色';
  if (level === 'ask') return '需要审批';
  if (level === 'full') return '完全访问权限';
  if (level === 'approval') return '工作区内修改';
  if (level === 'readonly') return '只读';
  return level;
}

/** 最近消息摘要：带发言人前缀，去掉换行。 */
function summarize(m: Message): string {
  const text = (m.content ?? '').replace(/\s+/g, ' ').trim();
  const who = m.role === 'user' ? '我' : (m.agent ?? '');
  const body = text.length > 34 ? `${text.slice(0, 34)}…` : text;
  return who ? `${who}：${body || '（附件）'}` : body || '（附件）';
}

function stampOf(m: Message | undefined, c: Conversation): number {
  const s = m?.createdAt ?? c.updatedAt;
  const v = new Date(s).getTime();
  return Number.isFinite(v) ? v : 0;
}

/** 侧栏时间：「9:41」「昨天」「10/21」 */
function relativeTime(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const now = new Date();
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();
  if (sameDay) {
    return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
  }
  const yest = new Date(now.getTime() - 86400000);
  if (
    d.getFullYear() === yest.getFullYear() &&
    d.getMonth() === yest.getMonth() &&
    d.getDate() === yest.getDate()
  ) {
    return '昨天';
  }
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

function monthDay(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

/** 额度重置日：本自然月最后一天的次日（下月 1 日）。 */
function nextResetLabel(): string {
  const now = new Date();
  const next = new Date(now.getFullYear(), now.getMonth() + 1, 1);
  return `${next.getMonth() + 1} 月 ${next.getDate()} 日`;
}

function lightenHex(hex: string, amount: number): string {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  const mix = (c: number) => Math.round(c + (255 - c) * amount);
  return `#${[mix(r), mix(g), mix(b)].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
}

/* ============================================================
   样式
   ============================================================ */

const PAGE_X = 20;

const s = StyleSheet.create({
  /* 顶部分段 */
  navFloat: {
    alignItems: 'center',
    paddingTop: 4,
    paddingBottom: 6,
    zIndex: 10,
  },

  /* 通用页容器 */
  page: {
    paddingHorizontal: PAGE_X,
    paddingBottom: 110,
  },

  /* 标题区（A3） */
  titleRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    marginTop: 18,
    minHeight: 78,
  },
  eyebrow: {
    textTransform: 'uppercase',
  },
  fab: {
    width: 46,
    height: 46,
    borderRadius: 23,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 4,
    shadowOpacity: 0.38,
    shadowRadius: 14,
    shadowOffset: { width: 0, height: 6 },
    elevation: 6,
  },

  /* Bento */
  heroActions: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 16,
    paddingVertical: 13,
    marginTop: 16,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  bentoRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 12,
    marginTop: 12,
  },
  // 两列：flexBasis 47% 而不是 flex:1——flex:1 不换行时会把三个卡挤成一排
  bentoCell: {
    flexBasis: '47%',
    flexGrow: 1,
    maxWidth: '48.5%',
  },
  smallActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 14,
    paddingHorizontal: 15,
    paddingVertical: 11,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  track: {
    height: 5,
    borderRadius: 99,
    overflow: 'hidden',
  },
  emptyCard: {
    marginTop: 22,
    borderRadius: 26,
    padding: 24,
    alignItems: 'center',
    gap: 6,
    borderWidth: StyleSheet.hairlineWidth,
  },
  tileHighlight: {
    position: 'absolute',
    top: 0,
    left: '12%',
    right: '12%',
    height: 1.5,
    borderTopLeftRadius: 8,
    borderTopRightRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.40)',
  },

  /* 对话页 */
  approvalRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    paddingHorizontal: 16,
    paddingVertical: 15,
  },
  warnDot: {
    width: 7,
    height: 7,
    borderRadius: 3.5,
  },
  convRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 14,
    paddingVertical: 13,
  },
  miniStack: {
    width: 46,
    height: 46,
    borderRadius: 14,
    padding: 3,
    alignItems: 'center',
    justifyContent: 'center',
  },
  unread: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  chatFooter: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    paddingHorizontal: PAGE_X,
    paddingTop: 12,
    paddingBottom: 12,
    alignItems: 'center',
  },
  primaryBtn: {
    height: 54,
    borderRadius: 999,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    paddingHorizontal: 32,
    minWidth: 220,
    shadowOpacity: 0.38,
    shadowRadius: 14,
    shadowOffset: { width: 0, height: 12 },
    elevation: 6,
  },

  /* 我页 · 英雄卡（A2）
     阴影放在外层：卡本身要 overflow:hidden 才能裁掉光晕 */
  heroShadow: {
    marginTop: 18,
    borderRadius: 28,
    shadowRadius: 19,
    shadowOffset: { width: 0, height: 18 },
    elevation: 10,
  },
  hero: {
    borderRadius: 28,
    overflow: 'hidden',
  },
  heroInner: {
    padding: 20,
  },
  glow: {
    position: 'absolute',
    borderRadius: 999,
  },
  glowA: {
    width: 230,
    height: 230,
    top: -120,
    left: -60,
    opacity: 0.3,
  },
  glowB: {
    width: 260,
    height: 260,
    top: -40,
    right: -110,
    opacity: 0.26,
  },
  meAvatar: {
    width: 58,
    height: 58,
    borderRadius: 20,
    alignItems: 'center',
    justifyContent: 'center',
  },
  quotaCard: {
    marginTop: 18,
    borderRadius: 16,
    padding: 14,
    backgroundColor: 'rgba(255,255,255,0.09)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.10)',
  },
  quotaTrack: {
    height: 6,
    borderRadius: 99,
    overflow: 'hidden',
    marginTop: 10,
  },

  /* 我页 · 菜单（A5） */
  menuRow: {
    paddingHorizontal: 14,
    paddingVertical: 13,
  },
  iconBrick: {
    width: 44,
    height: 44,
    borderRadius: 13,
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
  },

  /* 操作菜单 */
  scrim: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  sheet: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingTop: 8,
    paddingBottom: 8,
  },
  grabber: {
    width: 36,
    height: 4,
    borderRadius: 2,
    alignSelf: 'center',
    marginBottom: 14,
  },
  sheetRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 18,
    paddingVertical: 15,
  },
  cancel: {
    height: 50,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },

  /* 错误条 */
  banner: {
    position: 'absolute',
    left: 12,
    right: 12,
    bottom: 12,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingVertical: 11,
    paddingHorizontal: 12,
    borderRadius: 16,
  },
});
