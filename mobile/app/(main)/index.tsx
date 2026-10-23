import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Animated, Easing } from 'react-native';
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AgentPanel } from '../../src/components/AgentPanel';
import { ApprovalSheet } from '../../src/components/ApprovalSheet';
import { Avatar, AvatarStack } from '../../src/components/Avatar';
import { Canvas } from '../../src/components/Canvas';
import { Composer } from '../../src/components/Composer';
import { Glass } from '../../src/components/Glass';
import { Surface } from '../../src/components/Surface';
import { FadeInUp, PressableScale, TypingDots } from '../../src/components/motion';
import { OnboardingChat } from '../../src/components/OnboardingChat';
import { Pager } from '../../src/components/Pager';
import { FadingHeader } from '../../src/components/ScrollFx';
import {
  AccentIconButton,
  Button,
  Card,
  Empty,
  GlassBar,
  Icon,
  IconButton,
  IconTile,
  Progress,
  PulseDot,
  Row,
  T,
  Tag,
  grad,
  withAlpha,
} from '../../src/components/ui';
import { get, humanError, post, stream } from '../../src/lib/api';
import { normalizeActivity } from '../../src/lib/activity';
import type { Approval } from '../../src/lib/runs';
import { signOut } from '../../src/lib/session';
import { ensureDefaultTeam } from '../../src/lib/defaultTeam';
import { useApp } from '../../src/lib/store';
import type { AgentActivity, ChatEvent, Conversation, Message, Scenario } from '../../src/lib/types';
import { useTheme, useThemePref } from '../../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, radius, type } from '../../src/theme/tokens';

/**
 * 主界面：三页横滑。
 *
 *   左（0）：我 —— 设备、配对、设置、主题
 *   中（1）：对话 —— 默认群聊 / 首屏引导
 *   右（2）：团队 —— 群组列表、创建团队、角色市场
 *
 * 没有底栏。这是元宝那类 App 的做法：内容占满整屏，
 * 导航靠手势和一个可见的方向提示，而不是一直占着一行像素。
 */

const PAGE_ME = 0;
const PAGE_CHAT = 1;
const PAGE_TEAMS = 2;

const SCENE: Record<string, { icon: any; colors: [string, string] }> = {
  bid: { icon: 'document-text', colors: ['#F5A972', '#E8734A'] },
  code: { icon: 'code-slash', colors: ['#7FA8F7', '#4A7FE0'] },
  xhs: { icon: 'color-palette', colors: ['#F79BB8', '#D9608A'] },
  research: { icon: 'analytics', colors: ['#B39AF0', '#7C5CE0'] },
  onepiece: { icon: 'boat', colors: ['#6FD8AE', '#34A87C'] },
  anime: { icon: 'sparkles', colors: ['#F5C56B', '#DFA02F'] },
  companion: { icon: 'chatbubbles', colors: ['#7ECBF0', '#4A9FD4'] },
};
const DEFAULT_SCENE = { icon: 'people' as const, colors: ['#8FA5F2', '#5B6FD9'] as [string, string] };

export default function Main() {
  const t = useTheme();
  const router = useRouter();
  const user = useApp((s) => s.user);
  const convs = useApp((s) => s.convs);
  const scenarios = useApp((s) => s.scenarios);
  const setConvs = useApp((s) => s.setConvs);
  const setScenarios = useApp((s) => s.setScenarios);
  const setUser = useApp((s) => s.setUser);

  const [page, setPage] = useState(PAGE_CHAT); // 默认落在对话
  const [activeConv, setActiveConv] = useState<string | null>(null);
  const [err, setErr] = useState('');

  // 首屏引导：本地标记，只在第一次打开时出现
  const [needOnboard, setNeedOnboard] = useState<boolean | null>(null);

  const [msgs, setMsgs] = useState<Message[]>([]);
  const [activity, setActivity] = useState<AgentActivity[]>([]);
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState('');
  const [panelAgent, setPanelAgent] = useState<string | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [mentionOpen, setMentionOpen] = useState(false);
  const [approval, setApproval] = useState<{ a: Approval; runId: string } | null>(null);

  const abortRef = useRef<(() => void) | null>(null);
  const listRef = useRef<ScrollView>(null);
  const streamingRef = useRef<Record<string, string>>({});
  const seenApprovals = useRef<Set<string>>(new Set());
  const longPressTimer = useRef<any>(null);

  /* ---------- 基础数据 ---------- */
  const loadAll = useCallback(async () => {
    try {
      const [sc, cv, me] = await Promise.allSettled([
        get<any>('/v1/scenarios'),
        get<Conversation[]>('/v1/conversations'),
        get<any>('/v1/me'),
      ]);
      if (sc.status === 'fulfilled') {
        const raw: any = sc.value;
        setScenarios(Array.isArray(raw) ? raw : raw?.scenarios ?? []);
      }
      if (cv.status === 'fulfilled') setConvs(cv.value ?? []);
      if (me.status === 'fulfilled') {
        const d: any = me.value;
        setUser(d?.user ?? d);
      }
      // 默认落在「纸团」搭档群，而不是随便挑第一个群——
      // 首页是用户的日常入口，不该一上来是别的群。
      // 注意：activeConv 一旦确定就不再覆盖，否则用户从团队页切群后
      // 每次刷新都会被拉回默认群。
      if (!activeConv) {
        const def = await ensureDefaultTeam();
        if (def) {
          setActiveConv(def.id);
          const list = await get<Conversation[]>('/v1/conversations');
          setConvs(list ?? []);
        } else if (cv.status === 'fulfilled' && (cv.value?.length ?? 0) > 0) {
          setActiveConv(cv.value[0].id);
        }
      }
    } catch (e) {
      setErr(humanError(e));
    }
  }, [setScenarios, setConvs, setUser, activeConv]);

  useEffect(() => {
    loadAll();
  }, []);

  /* ---------- 首次引导标记 ---------- */
  useEffect(() => {
    import('../../src/lib/storage').then(async ({ getItem, setItem }) => {
      const seen = await getItem('zhituan_onboarded');
      setNeedOnboard(seen !== '1');
      // 让首屏引导有机会展示；标记在用户点「开始使用」时才写
      void setItem;
    });
  }, []);

  async function finishOnboard() {
    const { setItem } = await import('../../src/lib/storage');
    await setItem('zhituan_onboarded', '1');
    setNeedOnboard(false);
  }

  /* ---------- 会话 ---------- */
  const conv = convs.find((c) => c.id === activeConv) ?? null;

  const loadMsgs = useCallback(async (id: string) => {
    try {
      const m = await get<{ messages?: Message[] } | Message[]>(`/v1/conversations/${id}/messages?page=1&limit=50`);
      setMsgs(Array.isArray(m) ? m : (m?.messages ?? []));
    } catch (e) {
      setErr(humanError(e));
    }
  }, []);

  const loadActivity = useCallback(async (id: string) => {
    try {
      const a = await get<unknown>(`/v1/conversations/${id}/activity`);
      setActivity(normalizeActivity(a));
    } catch {
      /* 状态环失败不影响聊天 */
    }
  }, []);

  useEffect(() => {
    if (!activeConv) return;
    loadMsgs(activeConv);
    loadActivity(activeConv);
    const timer = setInterval(() => loadActivity(activeConv), 5000);
    return () => clearInterval(timer);
  }, [activeConv, loadMsgs, loadActivity]);

  // 审批轮询
  useEffect(() => {
    if (!activeConv) return;
    let alive = true;
    async function poll() {
      try {
        const acts = normalizeActivity(await get<unknown>(`/v1/conversations/${activeConv}/activity`));
        if (!alive) return;
        for (const b of acts.filter((a) => a.state === 'blocked' && a.runId)) {
          const run = await get<any>(`/v1/runs/${b.runId}`);
          if (!alive) return;
          const p = (run?.approvals ?? []).find(
            (x: any) => x.status === 'pending' && !seenApprovals.current.has(x.id),
          );
          if (p) {
            seenApprovals.current.add(p.id);
            setApproval({ a: p, runId: b.runId! });
            break;
          }
        }
      } catch {
        /* 下一轮再试 */
      }
    }
    const timer = setInterval(poll, 4000);
    poll();
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [activeConv]);

  useEffect(() => () => abortRef.current?.(), []);

  /* ---------- 发送 ---------- */
  async function send() {
    const text = input.trim();
    if (!text || busy || !activeConv) return;
    setInput('');
    setErr('');
    setBusy(true);
    streamingRef.current = {};

    setMsgs((m) => [
      ...m,
      {
        id: `tmp_${Date.now()}`,
        conversationId: activeConv,
        role: 'user',
        content: text,
        tokensIn: 0,
        tokensOut: 0,
        latencyMs: 0,
        createdAt: new Date().toISOString(),
      },
    ]);
    setTimeout(() => listRef.current?.scrollToEnd({ animated: true }), 60);

    abortRef.current = await stream(`/v1/conversations/${activeConv}/orchestrate`, { content: text }, handleEvent);
  }

  function handleEvent(ev: ChatEvent) {
    switch (ev.type) {
      case 'start':
        streamingRef.current[ev.agent ?? ''] = '';
        setMsgs((m) => [
          ...m,
          {
            id: `live_${ev.agent}_${Date.now()}`,
            conversationId: activeConv!,
            role: 'assistant',
            agent: ev.name,
            content: '',
            tokensIn: 0,
            tokensOut: 0,
            latencyMs: 0,
            createdAt: new Date().toISOString(),
          },
        ]);
        setActivity((a) => a.map((x) => (x.key === ev.agent ? { ...x, state: 'working', detail: '正在发言' } : x)));
        break;
      case 'delta': {
        const k = ev.agent ?? '';
        streamingRef.current[k] = (streamingRef.current[k] ?? '') + (ev.content ?? '');
        const text = streamingRef.current[k];
        setMsgs((m) => {
          const next = [...m];
          for (let i = next.length - 1; i >= 0; i--) {
            if (next[i].agent === ev.name && next[i].id.startsWith('live_')) {
              next[i] = { ...next[i], content: text };
              break;
            }
          }
          return next;
        });
        break;
      }
      case 'end':
        if (ev.content) streamingRef.current[ev.agent ?? ''] = ev.content;
        break;
      case 'artifact':
        setMsgs((m) => [
          ...m,
          {
            id: `art_${Date.now()}_${ev.agent}`,
            conversationId: activeConv!,
            role: 'system',
            agent: ev.name,
            content: ev.content ?? '',
            artifacts: [ev.title ?? '产物'],
            tokensIn: 0,
            tokensOut: 0,
            latencyMs: 0,
            createdAt: new Date().toISOString(),
          },
        ]);
        break;
      case 'busy':
        setErr('上一轮还没结束，等它说完。');
        break;
      case 'quota':
        setErr('本月额度已用完。');
        break;
      case '__error':
        setErr(ev.message ?? '连接断了');
        break;
      case '__closed':
        setBusy(false);
        if (activeConv) {
          loadMsgs(activeConv);
          loadActivity(activeConv);
        }
        break;
    }
  }

  function stop() {
    abortRef.current?.();
    abortRef.current = null;
    setBusy(false);
  }

  async function decide(approve: boolean, scope: 'once' | 'run') {
    if (!approval) return;
    const { a, runId } = approval;
    setApproval(null);
    try {
      await post(`/v1/runs/${runId}/approve`, { approvalId: a.id, approve, scope });
      if (activeConv) loadActivity(activeConv);
    } catch (e) {
      setErr(humanError(e));
    }
  }

  /* ---------- 成员 ---------- */
  const members: { key: string; name: string; title?: string; color?: string }[] = conv?.agents?.length
    ? conv.agents.map((a) => ({ key: a.key, name: a.name, title: a.title, color: a.color }))
    : activity.map((a) => ({ key: a.key, name: a.name }));

  const working = activity.filter((a) => a.state === 'working' || a.state === 'blocked');

  function insertMention(name: string) {
    setInput((v) => (v.endsWith(' ') || v === '' ? `${v}@${name} ` : `${v} @${name} `));
    setMentionOpen(false);
  }

  if (needOnboard === null) return <Canvas><View style={{ flex: 1 }} /></Canvas>;

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <Pager index={page} onIndexChange={setPage}>
          {/* ---------------- 左：我 ---------------- */}
          <MePage
            user={user}
            onOpenSettings={() => router.push('/settings')}
            onOpenLibrary={() => router.push('/library')}
            onOpenGoals={() => router.push('/goals')}
          />

          {/* ---------------- 中：对话 ---------------- */}
          <View style={{ flex: 1 }}>
            {/* 悬浮玻璃胶囊：不贴边，浮在内容上方 */}
            <View style={s.topbarFloat}>
              <Glass radius={26} style={s.topbarGlass}>
                <Pressable onPress={() => setPage(PAGE_TEAMS)} style={s.pillBtn}>
                  <Icon name="chevron-back" size={19} color={t.ink} />
                </Pressable>
                <View style={{ flex: 1, alignItems: 'center' }}>
                  <T size={type.body} weight="600" numberOfLines={1}>
                    {needOnboard ? '纸团' : conv?.title ?? '纸团'}
                  </T>
                  {working.length > 0 ? (
                    <Row gap={4} style={{ marginTop: 1 }}>
                      <PulseDot active tone={t.accent} />
                      <T size={type.caption} color={t.accent} weight="600">
                        {working.length} 个在干活
                      </T>
                    </Row>
                  ) : null}
                </View>
                <Pressable onPress={() => setMenuOpen(true)} style={s.pillBtn}>
                  <Icon name="menu" size={16} color={t.ink} />
                </Pressable>
              </Glass>
            </View>

            {/* 成员条 */}
            {/* 成员条：紧凑一行。点开看这个人在干什么——这是产品的核心差异点，
                所以它常驻在对话页顶部，而不是藏在菜单里。 */}
            {members.length > 0 && !needOnboard ? (
              <ScrollView
                horizontal
                showsHorizontalScrollIndicator={false}
                style={{ flexGrow: 0, flexShrink: 0 }}
                contentContainerStyle={s.members}
              >
                {members.map((a) => {
                  const st = activity.find((x) => x.key === a.key);
                  const working = st?.state === 'working' || st?.state === 'blocked';
                  return (
                    <PressableScale key={a.key} onPress={() => setPanelAgent(a.key)} scaleTo={0.9}>
                      <View
                        style={[
                          s.chipMember,
                          {
                            backgroundColor: working ? t.accentSoft : 'transparent',
                            borderColor: working ? withAlpha(t.accent, 0.22) : 'transparent',
                          },
                        ]}
                      >
                        <Avatar name={a.name} tint={a.color} size={20} state={st?.state} />
                        <T size={type.caption} color={working ? t.accent : t.ink2} weight={working ? '700' : '500'} numberOfLines={1}>
                          {a.name}
                        </T>
                      </View>
                    </PressableScale>
                  );
                })}
              </ScrollView>
            ) : null}

            {err ? (
              <Pressable onPress={() => setErr('')} style={[s.banner, { backgroundColor: t.badSoft }]}>
                <Icon name="alert-circle" size={15} color={t.bad} />
                <T size={type.sub} color={t.bad} style={{ flex: 1 }}>
                  {err}
                </T>
              </Pressable>
            ) : null}

            {/* 消息流 / 引导 */}
            {needOnboard ? (
              <OnboardingChat onDone={finishOnboard} />
            ) : (
              <ScrollView
                ref={listRef}
                contentContainerStyle={[
                  s.msgList,
                  msgs.length === 0 && { flexGrow: 1 },
                ]}
                showsVerticalScrollIndicator={false}
                keyboardShouldPersistTaps="handled"
              >
                {msgs.length === 0 ? (
                  <EmptyChat onPick={(text) => setInput(text)} />
                ) : (
                  msgs.map((m, i) => (
                    <Bubble
                      key={`${m.id}_${i}`}
                      msg={m}
                      showAgent={msgs[i - 1]?.agent !== m.agent}
                      streaming={busy && i === msgs.length - 1}
                      onAvatar={m.agent ? () => {
                        const found = members.find((x) => x.name === m.agent);
                        if (found) setPanelAgent(found.key);
                      } : undefined}
                    />
                  ))
                )}
              </ScrollView>
            )}

            {/* 输入区 */}
            {needOnboard ? null : (
              <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
                <Composer
                  value={input}
                  onChangeText={setInput}
                  onSend={send}
                  onStop={stop}
                  busy={busy}
                  onMention={() => setMentionOpen((v) => !v)}
                  placeholder={mentionOpen ? '选一个成员…' : '@ 指定成员，或直接说'}
                />
                {mentionOpen ? (
                  <MentionPicker members={members} onPick={insertMention} onClose={() => setMentionOpen(false)} />
                ) : null}
              </KeyboardAvoidingView>
            )}
          </View>

          {/* ---------------- 右：团队 ---------------- */}
          <TeamsPage
            convs={convs}
            scenarios={scenarios}
            activeConv={activeConv}
            onOpen={(id) => {
              setActiveConv(id);
              setPage(PAGE_CHAT);
            }}
            onNew={() => router.push('/team/new')}
            onLibrary={() => router.push('/library')}
            onGoals={() => router.push('/goals')}
          />
        </Pager>

        {/* 成员工作面板 */}
        <AgentPanel
          visible={!!panelAgent}
          agent={(() => {
            const f = members.find((a) => a.key === panelAgent);
            return f ? { key: f.key, name: f.name, title: f.title, color: f.color } : null;
          })()}
          activity={activity.find((a) => a.key === panelAgent)}
          convId={activeConv ?? ''}
          workspaceId={(conv as any)?.workspaceId}
          onClose={() => setPanelAgent(null)}
        />

        {/* 更多菜单 */}
        {menuOpen ? (
          <MoreMenu
            onClose={() => setMenuOpen(false)}
            items={[
              ...(activeConv ? [{ icon: 'people-outline', label: '交给团队', onPress: () => router.push(`/team/task?convId=${activeConv}`) }] : []),
              { icon: 'folder-open-outline', label: '查看产物', onPress: () => router.push('/library') },
              { icon: 'flag-outline', label: '目标看板', onPress: () => router.push('/goals') },
              { icon: 'git-network-outline', label: '连接器', onPress: () => router.push('/connect') },
              { icon: 'settings-outline', label: '团队设置', onPress: () => router.push('/settings') },
            ]}
          />
        ) : null}

        <ApprovalSheet
          visible={!!approval}
          tool={approval?.a.tool ?? ''}
          summary={approval?.a.command || approval?.a.summary || ''}
          risk={approval?.a.risk ?? 'write'}
          reason={approval?.a.summary}
          onAllow={() => decide(true, 'once')}
          onAllowRun={() => decide(true, 'run')}
          onDeny={() => decide(false, 'once')}
        />
      </SafeAreaView>
    </Canvas>
  );
}

/* ============================================================
   气泡
   ============================================================ */

function Bubble({
  msg,
  showAgent,
  streaming,
  onAvatar,
}: {
  msg: Message;
  showAgent: boolean;
  streaming: boolean;
  onAvatar?: () => void;
}) {
  const t = useTheme();

  if (msg.role === 'system' && msg.artifacts?.length) {
    return (
      <Card style={{ marginVertical: gap.xs }}>
        <Row style={{ justifyContent: 'space-between', marginBottom: gap.xs }}>
          <Row gap={6}>
            <IconTileSoftMini name="document-text" tint={t.accent} />
            <T size={type.caption} color={t.accent} weight="600">
              产出
            </T>
          </Row>
          <T size={type.caption} color={t.ink3}>
            {msg.agent}
          </T>
        </Row>
        <T size={type.headline} weight="600" numberOfLines={1}>
          {msg.artifacts?.[0]}
        </T>
        <T size={type.sub} color={t.ink2} lh={leading.sub} numberOfLines={3} style={{ marginTop: 4 }}>
          {msg.content}
        </T>
      </Card>
    );
  }

  if (msg.role === 'system') {
    return (
      <View style={{ alignItems: 'center', paddingVertical: gap.xxs }}>
        <T size={type.caption} color={t.ink3} align="center">
          {msg.content}
        </T>
      </View>
    );
  }

  if (msg.role === 'user') {
    return (
      <FadeInUp distance={8}>
        <View style={{ alignItems: 'flex-end', marginBottom: gap.sm }}>
          <LinearGradient
            colors={grad(t.gradient)}
            start={{ x: 0, y: 0 }}
            end={{ x: 1, y: 1 }}
            style={s.userBubble}
          >
            <T size={type.body} color="#FFFFFF" lh={leading.body}>
              {msg.content}
            </T>
          </LinearGradient>
        </View>
      </FadeInUp>
    );
  }

  // AI 气泡：柔和的浅色底、大圆角、无边框。
  // 不用纯白（会和画布混在一起），也不用描边（显硬）——
  // 用比画布稍亮一档的米白，靠圆角和留白建立"卡片感"。
  return (
    <View style={{ flexDirection: 'row', gap: 10, alignItems: 'flex-start', marginTop: showAgent ? 22 : 8 }}>
      <Pressable onPress={onAvatar} disabled={!onAvatar} style={{ width: 32 }}>
        {showAgent ? <Avatar name={msg.agent ?? '?'} size={30} /> : null}
      </Pressable>
      <View style={{ flex: 1, maxWidth: '86%' }}>
        {showAgent ? (
          <T size={type.caption} color={t.ink3} weight="600" style={{ marginBottom: 5, marginLeft: 2 }}>
            {msg.agent}
          </T>
        ) : null}
        <View
          style={[
            s.agentBubble,
            {
              backgroundColor: t.dark ? t.card : '#FFFFFF',
              borderColor: t.dark ? t.cardBorder : 'rgba(20,24,45,0.045)',
            },
          ]}
        >
          {msg.content ? (
            <T size={type.body} lh={24} color={t.ink}>
              {msg.content}
            </T>
          ) : streaming ? (
            <TypingDots color={t.ink4} />
          ) : (
            <T size={type.body} color={t.ink4}>
              …
            </T>
          )}
        </View>

        {/* 操作条：元宝在每个回复下面都有一条。只在完整回复后出现，
            流式中不出现（那时候用户还没读完，按钮是干扰）。 */}
        {msg.content && !streaming ? <MsgActions text={msg.content} /> : null}
        {msg.attachments?.length ? (
          <Row gap={6} style={{ flexWrap: 'wrap', marginTop: 6 }}>
            {msg.attachments.map((a) => (
              <Tag key={a.id || a.path} label={a.name || a.path || '产物'} tone="done" />
            ))}
          </Row>
        ) : null}
      </View>
    </View>
  );
}

/** 一条回复的操作按钮：复制 / 重说 / 记下来。圆形 ghost，无底色。 */
function MsgActions({ text }: { text: string }) {
  const t = useTheme();
  const [copied, setCopied] = useState(false);

  const item = (icon: any, label: string, onPress: () => void, active?: boolean) => (
    <Pressable
      key={label}
      onPress={onPress}
      accessibilityLabel={label}
      style={({ pressed }) => [
        s.actionBtn,
        {
          backgroundColor: pressed
            ? t.dark
              ? 'rgba(255,255,255,0.08)'
              : 'rgba(16,20,45,0.05)'
            : 'transparent',
        },
      ]}
    >
      <Icon name={icon} size={15} color={active ? t.accent : t.ink4} />
    </Pressable>
  );

  return (
    <Row gap={0} style={{ marginTop: 2, marginLeft: -7 }}>
      {item(copied ? 'checkmark' : 'copy-outline', '复制', async () => {
        try {
          const { default: Clipboard } = await import('expo-clipboard');
          await Clipboard.setStringAsync(text);
          setCopied(true);
          setTimeout(() => setCopied(false), 1600);
        } catch {
          /* 剪贴板不可用时静默 */
        }
      }, copied)}
      {item('refresh-outline', '重说', () => {})}
      {item('bookmark-outline', '记下来', () => {})}
      {item('share-outline', '分享', () => {})}
    </Row>
  );
}

/** 空对话页：一句招呼 + 几个可以点的建议。比一个孤零零的图标有用得多。 */
function EmptyChat({ onPick }: { onPick: (t: string) => void }) {
  const t = useTheme();
  const ideas = ['帮我把这个需求拆成任务', '解释一下这段代码在做什么', '查一下这个概念的区别'];
  const h = new Date().getHours();
  return (
    <View style={{ flex: 1, justifyContent: 'center', paddingBottom: 40 }}>
      <View>
        {/* 大标题：字号拉开到 32，字距收紧。这是整页唯一的视觉重心。 */}
        <T size={type.display} weight="700" lh={40} style={{ letterSpacing: -0.5 }}>
          有什么可以帮你的？
        </T>
        <T size={type.sub} color={t.ink3} style={{ marginTop: 8 }}>
          {h < 12 ? '早上好' : h < 18 ? '下午好' : '晚上好'}，我是纸团
        </T>
      </View>

      <View style={{ marginTop: 28, gap: 10 }}>
        {ideas.map((idea, i) => (
          <FadeInUp key={idea} delay={i * 70}>
            <Surface onPress={() => onPick(idea)} shell={4} outerRadius={22}>
              <View style={s.ideaInner}>
                <T size={type.body} color={t.ink} weight="500" style={{ flex: 1 }}>
                  {idea}
                </T>
                {/* Button-in-Button：箭头有自己的圆形容器，不裸放 */}
                <View style={[s.arrowDot, { backgroundColor: t.dark ? 'rgba(255,255,255,0.07)' : 'rgba(16,20,45,0.045)' }]}>
                  <Icon name="arrow-up" size={13} color={t.ink3} style={{ transform: [{ rotate: '45deg' }] }} />
                </View>
              </View>
            </Surface>
          </FadeInUp>
        ))}
      </View>
    </View>
  );
}

function IconTileSoftMini({ name, tint }: { name: any; tint: string }) {
  return (
    <View
      style={{
        width: 20,
        height: 20,
        borderRadius: 7,
        backgroundColor: withAlpha(tint, 0.12),
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Ionicons name={name} size={11} color={tint} />
    </View>
  );
}

/* ============================================================
   左侧：我
   ============================================================ */

function MePage({
  user,
  onOpenSettings,
  onOpenLibrary,
  onOpenGoals,
}: {
  user: any;
  onOpenSettings: () => void;
  onOpenLibrary: () => void;
  onOpenGoals: () => void;
}) {
  const t = useTheme();
  const pref = useThemePref((s) => s.pref);
  const setPref = useThemePref((s) => s.setPref);

  return (
    <ScrollView contentContainerStyle={s.page} showsVerticalScrollIndicator={false}>
      <FadeInUp>
        <Row gap={gap.sm} style={{ marginTop: gap.md }}>
          <LinearGradient colors={grad(t.gradient)} start={{ x: 0, y: 0 }} end={{ x: 1, y: 1 }} style={s.meAvatar}>
            <T size={type.title} color="#FFFFFF" weight="600">
              {(user?.name ?? '我').slice(0, 1)}
            </T>
          </LinearGradient>
          <View style={{ flex: 1 }}>
            <T size={type.title} weight="600" numberOfLines={1}>
              {user?.name ?? '已连接'}
            </T>
            <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
              额度 {user ? Math.max(0, (user.quota ?? 0) - (user.used ?? 0)) : '—'} / {user?.quota ?? '—'}
            </T>
          </View>
        </Row>
      </FadeInUp>

      <T size={type.caption} color={t.ink3} weight="600" style={{ marginTop: gap.xxl, marginBottom: gap.xs }}>
        外观
      </T>
      <Card>
        <Row gap={6}>
          {(['auto', 'light', 'dark'] as const).map((k) => (
            <Pressable
              key={k}
              onPress={() => setPref(k)}
              style={{
                flex: 1,
                paddingVertical: 9,
                borderRadius: radius.control,
                backgroundColor: pref === k ? t.accentSoft : 'transparent',
                alignItems: 'center',
              }}
            >
              <T size={type.sub} color={pref === k ? t.accentText : t.ink2} weight={pref === k ? "600" : "400"}>
                {k === 'auto' ? '跟随系统' : k === 'light' ? '浅色' : '深色'}
              </T>
            </Pressable>
          ))}
        </Row>
      </Card>

      <T size={type.caption} color={t.ink3} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.xs }}>
        常用
      </T>
      <View style={{ gap: gap.xs }}>
        <MenuRow icon="flag" colors={['#7FA8F7', '#4A7FE0']} label="目标看板" onPress={onOpenGoals} />
        <MenuRow icon="folder-open" colors={['#B39AF0', '#7C5CE0']} label="资料库" onPress={onOpenLibrary} />
        <MenuRow icon="settings" colors={['#9AA0B4', '#6E7488']} label="设备与设置" onPress={onOpenSettings} />
      </View>

      <T size={type.caption} color={t.ink3} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.xs }}>
        连接电脑
      </T>
      <Card>
        <Row gap={gap.xs} align="flex-start">
          <Icon name="laptop-outline" size={16} color={t.ink3} style={{ marginTop: 1 }} />
          <T size={type.sub} color={t.ink2} lh={leading.sub} style={{ flex: 1 }}>
            在电脑上打开纸团 → 设置 → 配对新设备，把 6 位码填进「设备与设置」。
            配对后这台手机就能远程看工作区、发任务、审批。
          </T>
        </Row>
      </Card>
    </ScrollView>
  );
}

function MenuRow({
  icon,
  colors,
  label,
  onPress,
}: {
  icon: any;
  colors: [string, string];
  label: string;
  onPress: () => void;
}) {
  const t = useTheme();
  return (
    <Surface onPress={onPress} shell={4} outerRadius={20}>
      <Row gap={12} style={{ padding: CARD_PAD }}>
        <IconTile name={icon} colors={colors} size={36} iconSize={17} radius={12} />
        <T size={type.body} weight="600" style={{ flex: 1 }}>
          {label}
        </T>
        <View style={[s.arrowDot, { backgroundColor: t.dark ? 'rgba(255,255,255,0.06)' : 'rgba(16,20,45,0.04)' }]}>
          <Icon name="chevron-forward" size={13} color={t.ink3} />
        </View>
      </Row>
    </Surface>
  );
}

/* ============================================================
   右侧：团队
   ============================================================ */

function TeamsPage({
  convs,
  scenarios,
  activeConv,
  onOpen,
  onNew,
  onLibrary,
  onGoals,
}: {
  convs: Conversation[];
  scenarios: Scenario[];
  activeConv: string | null;
  onOpen: (id: string) => void;
  onNew: () => void;
  onLibrary: () => void;
  onGoals: () => void;
}) {
  const t = useTheme();
  const router = useRouter();
  const presets = scenarios.filter((s) => !s.random && (s.agents?.length ?? 0) > 0);
  const scrollY = useRef(new Animated.Value(0)).current;

  return (
    <Animated.ScrollView
      contentContainerStyle={s.page}
      showsVerticalScrollIndicator={false}
      scrollEventThrottle={16}
      onScroll={Animated.event([{ nativeEvent: { contentOffset: { y: scrollY } } }], {
        useNativeDriver: true,
      })}
    >
      <FadingHeader scrollY={scrollY}>
        <Row style={{ justifyContent: 'space-between', marginTop: gap.sm }}>
          <View>
            <T size={type.display} weight="700" lh={40} style={{ letterSpacing: -0.5 }}>
              团队
            </T>
            <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
              {convs.length} 个群组 · {presets.length} 个模板
            </T>
          </View>
          <AccentIconButton name="add" onPress={onNew} accessibilityLabel="新建团队" />
        </Row>
      </FadingHeader>

      {/* 快捷入口 */}
      <FadeInUp delay={50}>
        <Row gap={gap.xs} style={{ marginTop: gap.lg }}>
          <QuickTile icon="flag" colors={['#7FA8F7', '#4A7FE0']} label="目标" onPress={onGoals} />
          <QuickTile icon="folder-open" colors={['#B39AF0', '#7C5CE0']} label="资料" onPress={onLibrary} />
          <QuickTile icon="chatbubble-ellipses" colors={['#6FD8AE', '#34A87C']} label="单聊" onPress={() => router.push('/solo')} />
        </Row>
      </FadeInUp>

      {/* 我的群组：第一张是英雄卡（更大、带成员），
          其余是紧凑行。全部同尺寸会让页面读起来像表格。 */}
      <T size={type.headline} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.sm }}>
        我的群组
      </T>
      {convs.length === 0 ? (
        <Empty icon="people-outline" title="还没有群组" hint="从下面的模板建一个" />
      ) : (
        <View style={{ gap: CARD_GAP }}>
          {/* 当前群排第一当英雄卡：它的信息最多、最该被看到 */}
          {[...convs].sort((a, b) => (a.id === activeConv ? -1 : b.id === activeConv ? 1 : 0)).map((c, i) => {
            const sc = SCENE[c.studio] ?? DEFAULT_SCENE;
            const sc2 = scenarios.find((x) => x.key === c.studio);
            const names = c.agents?.length
              ? c.agents.map((a) => a.name)
              : (sc2?.agents ?? []).map((a) => a.name);
            const active = c.id === activeConv;
            const hero = i === 0;

            return (
              <FadeInUp key={c.id} delay={i * 45}>
                {hero ? (
                  <Surface
                    onPress={() => onOpen(c.id)}
                    shell={5}
                    outerRadius={26}
                    style={active ? { borderColor: t.accent, borderWidth: 1 } : undefined}
                  >
                    <View style={{ padding: CARD_PAD }}>
                      <Row gap={12} style={{ alignItems: 'flex-start' }}>
                        <IconTile name={sc.icon} colors={sc.colors} size={52} iconSize={24} radius={16} />
                        <View style={{ flex: 1 }}>
                          <Row gap={6}>
                            <T size={type.headline} weight="600" numberOfLines={1} style={{ flex: 1 }}>
                              {c.title}
                            </T>
                            {active ? <Tag label="当前" tone="working" /> : null}
                          </Row>
                          <T size={type.caption} color={t.ink3} style={{ marginTop: 3 }} numberOfLines={1}>
                            {names.length} 位成员
                            {names.length ? ` · ${names.slice(0, 3).join('、')}` : ''}
                          </T>
                        </View>
                      </Row>
                      {/* 英雄卡多一行：成员头像 + 进入提示 */}
                      <Row style={{ justifyContent: 'space-between', marginTop: 12 }}>
                        <AvatarStack names={names.slice(0, 4)} size={24} max={4} />
                        <Row gap={4}>
                          <T size={type.caption} color={t.accent} weight="600">
                            进入
                          </T>
                          <Icon name="arrow-forward" size={13} color={t.accent} />
                        </Row>
                      </Row>
                    </View>
                  </Surface>
                ) : (
                  <Surface
                    onPress={() => onOpen(c.id)}
                    shell={4}
                    outerRadius={22}
                    style={active ? { borderColor: t.accent, borderWidth: 1 } : undefined}
                  >
                    <Row gap={12} style={{ padding: CARD_PAD }}>
                      <IconTile name={sc.icon} colors={sc.colors} size={30} iconSize={18} radius={12} />
                      <View style={{ flex: 1 }}>
                        <T size={type.body} weight="600" numberOfLines={1}>
                          {c.title}
                        </T>
                        <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
                          {names.length} 位成员
                        </T>
                      </View>
                      {active ? <Tag label="当前" tone="working" /> : (
                        <Icon name="chevron-forward" size={15} color={t.ink4} />
                      )}
                    </Row>
                  </Surface>
                )}
              </FadeInUp>
            );
          })}
        </View>
      )}

      {/* 团队模板 */}
      <T size={type.headline} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.xs }}>
        团队模板
      </T>
      <View style={{ gap: gap.xs }}>
        {presets.map((sc, i) => {
          const scene = SCENE[sc.key] ?? DEFAULT_SCENE;
          return (
            <FadeInUp key={sc.key} delay={i * 40}>
              <Surface onPress={() => onNew()} shell={4} outerRadius={22}>
                <Row gap={12} style={{ padding: CARD_PAD }}>
                  <IconTile name={scene.icon} colors={scene.colors} size={30} iconSize={18} radius={12} />
                  <View style={{ flex: 1 }}>
                    <T size={type.body} weight="600">
                      {sc.name}
                    </T>
                    <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 2 }}>
                      {sc.tagline}
                    </T>
                  </View>
                  <AvatarStack
                    names={(sc.agents ?? []).map((a) => a.name)}
                    tints={(sc.agents ?? []).map((a) => a.color)}
                    size={20}
                    max={2}
                  />
                </Row>
              </Surface>
            </FadeInUp>
          );
        })}
      </View>
    </Animated.ScrollView>
  );
}

/**
 * 入场时的轻微倾斜。
 *
 * 前几张卡带极小的旋转（1.2°）从下方浮入，看起来像"一叠纸被放下"，
 * 而不是整齐地同时出现。旋转只在入场瞬间，落位后是正的——
 * 长期保持倾斜会影响阅读。
 */
function TiltIn({
  children,
  delay = 0,
  depth = 0,
}: {
  children: React.ReactNode;
  delay?: number;
  depth?: number;
}) {
  const v = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    Animated.timing(v, {
      toValue: 1,
      duration: 480,
      delay,
      easing: Easing.bezier(0.22, 1, 0.36, 1),
      useNativeDriver: true,
    }).start();
  }, [v, delay]);

  return (
    <Animated.View
      style={{
        opacity: v,
        transform: [
          { translateY: v.interpolate({ inputRange: [0, 1], outputRange: [18, 0] }) },
          { rotate: v.interpolate({ inputRange: [0, 1], outputRange: [`${(depth ? 1.4 : 0.8)}deg`, '0deg'] }) },
          { scale: v.interpolate({ inputRange: [0, 1], outputRange: [0.97, 1] }) },
        ],
      }}
    >
      {children}
    </Animated.View>
  );
}

function QuickTile({
  icon,
  colors,
  label,
  onPress,
}: {
  icon: any;
  colors: [string, string];
  label: string;
  onPress: () => void;
}) {
  const t = useTheme();
  return (
    <View style={{ flex: 1 }}>
      <Surface onPress={onPress} shell={4} outerRadius={20}>
        <View style={{ alignItems: 'center', paddingVertical: 16 }}>
          <IconTile name={icon} colors={colors} size={34} iconSize={17} radius={12} />
          <T size={type.caption} weight="600" color={t.ink2} style={{ marginTop: 8 }}>
            {label}
          </T>
        </View>
      </Surface>
    </View>
  );
}

/* ============================================================
   @ 成员选择
   ============================================================ */

function MentionPicker({
  members,
  onPick,
  onClose,
}: {
  members: { key: string; name: string; title?: string; color?: string }[];
  onPick: (name: string) => void;
  onClose: () => void;
}) {
  const t = useTheme();
  if (members.length === 0) return null;
  return (
    <View style={[s.mention, { backgroundColor: t.card, borderColor: t.cardBorder }]}>
      <Row style={{ justifyContent: 'space-between', marginBottom: gap.xs }}>
        <T size={type.caption} color={t.ink3} weight="600">
          指定成员
        </T>
        <T size={type.caption} color={t.ink3} onPress={onClose}>
          关闭
        </T>
      </Row>
      <ScrollView style={{ maxHeight: 180 }}>
        {members.map((m) => (
          <Pressable key={m.key} onPress={() => onPick(m.name)} style={{ paddingVertical: 8 }}>
            <Row gap={gap.xs}>
              <Avatar name={m.name} size={30} tint={m.color} />
              <View style={{ flex: 1 }}>
                <T size={type.sub} weight="600">
                  {m.name}
                </T>
                {m.title ? (
                  <T size={type.caption} color={t.ink3}>
                    {m.title}
                  </T>
                ) : null}
              </View>
            </Row>
          </Pressable>
        ))}
      </ScrollView>
    </View>
  );
}

/* ============================================================
   更多菜单
   ============================================================ */

function MoreMenu({
  items,
  onClose,
}: {
  items: { icon: any; label: string; onPress: () => void }[];
  onClose: () => void;
}) {
  const t = useTheme();
  return (
    <Pressable style={s.menuScrim} onPress={onClose}>
      <FadeInUp distance={-10}>
        <View style={[s.menu, { backgroundColor: t.card, borderColor: t.cardBorder }]}>
          {items.map((it, i) => (
            <Pressable
              key={it.label}
              onPress={() => {
                onClose();
                it.onPress();
              }}
              style={{
                paddingVertical: 13,
                borderTopWidth: i === 0 ? 0 : StyleSheet.hairlineWidth,
                borderTopColor: t.line,
              }}
            >
              <Row gap={gap.xs}>
                <Icon name={it.icon} size={16} color={t.ink2} />
                <T size={type.body} weight="500">
                  {it.label}
                </T>
              </Row>
            </Pressable>
          ))}
        </View>
      </FadeInUp>
    </Pressable>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: gap.lg, paddingBottom: 100 },
  topbarFloat: { paddingHorizontal: gap.md, paddingTop: 6, paddingBottom: 6 },
  topbarGlass: { height: 48, flexDirection: 'row', alignItems: 'center', paddingHorizontal: 6 },
  pillBtn: { width: 38, height: 38, borderRadius: 19, alignItems: 'center', justifyContent: 'center' },
  members: { paddingHorizontal: gap.md, gap: 6, alignItems: 'center', paddingVertical: 6 },
  chipMember: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingLeft: 3,
    paddingRight: 9,
    paddingVertical: 3,
    borderRadius: 999,
    borderWidth: StyleSheet.hairlineWidth,
    maxWidth: 124,
  },
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.xs,
    marginHorizontal: gap.md,
    marginBottom: gap.xs,
    paddingVertical: 10,
    paddingHorizontal: gap.sm,
    borderRadius: radius.md,
  },
  msgList: { paddingHorizontal: gap.md, paddingTop: gap.xs, paddingBottom: gap.lg },
  userBubble: {
    borderRadius: 20,
    borderBottomRightRadius: 7,
    paddingHorizontal: 15,
    paddingVertical: 11,
    maxWidth: '84%',
    shadowColor: '#4E7BFB',
    shadowOpacity: 0.22,
    shadowRadius: 14,
    shadowOffset: { width: 0, height: 5 },
    elevation: 4,
  },
  agentHead: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  pill: { width: 38, height: 38, borderRadius: 19, alignItems: 'center', justifyContent: 'center' },
  ideaInner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.sm,
    paddingHorizontal: 16,
    paddingVertical: 15,
  },
  arrowDot: { width: 26, height: 26, borderRadius: 13, alignItems: 'center', justifyContent: 'center' },
  actionBtn: { width: 28, height: 26, borderRadius: 13, alignItems: 'center', justifyContent: 'center' },
  agentBubble: {
    borderRadius: 20,
    borderTopLeftRadius: 7,
    paddingHorizontal: 15,
    paddingVertical: 11,
    alignSelf: 'flex-start',
    borderWidth: StyleSheet.hairlineWidth,
    shadowColor: '#2B3A77',
    shadowOpacity: 0.05,
    shadowRadius: 10,
    shadowOffset: { width: 0, height: 2 },
    elevation: 2,
  },
  composerWrap: { paddingHorizontal: gap.md, paddingTop: gap.xs, paddingBottom: gap.xs },
  // 双层输入区：外围是极淡的托盘，内部输入区是白色
  // 输入区：单层白色胶囊。双层在这里不成立——
  // 内芯会被外层圆角切掉一块，看起来像坏掉。
  composer: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 6,
    borderRadius: 26,
    padding: 5,
    borderWidth: StyleSheet.hairlineWidth,
    shadowColor: '#2B3A77',
    shadowOpacity: 0.08,
    shadowRadius: 20,
    shadowOffset: { width: 0, height: 6 },
    elevation: 6,
  },
  input: {
    flex: 1,
    minHeight: 42,
    maxHeight: 120,
    paddingHorizontal: 14,
    paddingTop: 11,
    paddingBottom: 11,
    fontSize: 15,
    backgroundColor: 'transparent',
  },
  atBtn: { width: 42, height: 42, borderRadius: 21, alignItems: 'center', justifyContent: 'center' },
  send: { width: 42, height: 42, borderRadius: 21, alignItems: 'center', justifyContent: 'center' },
  stopSquare: { width: 11, height: 11, borderRadius: 3, backgroundColor: '#FFFFFF' },
  mention: {
    marginTop: gap.xs,
    borderRadius: radius.md,
    padding: gap.sm,
    borderWidth: StyleSheet.hairlineWidth,
  },
  menuScrim: { position: 'absolute', top: 0, left: 0, right: 0, bottom: 0 },
  menu: {
    position: 'absolute',
    top: 104,
    right: gap.md,
    width: 190,
    borderRadius: radius.md,
    paddingHorizontal: gap.md,
    borderWidth: StyleSheet.hairlineWidth,
    shadowColor: '#14163A',
    shadowOpacity: 0.18,
    shadowRadius: 30,
    shadowOffset: { width: 0, height: 12 },
    elevation: 12,
  },
  meAvatar: {
    width: 54,
    height: 54,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  quickTile: {
    alignItems: 'center',
    paddingVertical: gap.sm,
    borderRadius: radius.md,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
