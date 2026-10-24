import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Alert,
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AddMemberSheet } from '../../src/components/AddMemberSheet';
import { AgentPanel } from '../../src/components/AgentPanel';
import { ApprovalTray } from '../../src/components/ApprovalTray';
import { Avatar } from '../../src/components/Avatar';
import { GroupInfoPanel } from '../../src/components/GroupInfoPanel';
import {
  ChatHeader,
  FileCard,
  MemberBar,
  MemberBubble,
  UserBubble,
  memberColor,
  withAlpha,
  type BarMember,
  type MemberState,
} from '../../src/components/chat';
import { TypingDots } from '../../src/components/motion';
import { Icon, T } from '../../src/components/ui';
import { get, humanError, patch, post, stream } from '../../src/lib/api';
import type { Approval } from '../../src/lib/runs';
import { useApp } from '../../src/lib/store';
import type {
  AgentActivity,
  ArtifactRef,
  ChatEvent,
  Conversation,
  Goal,
  Message,
} from '../../src/lib/types';
import { useTheme } from '../../src/theme/theme';
import { gap, lead, radius, shadow, type, weight } from '../../src/theme/tokens';

/**
 * 群聊主界面（设计规范 §6.1-6.6 / §9 第 1-2 屏）。
 *
 * 结构：
 *   顶栏（玻璃，群名 + 状态句）
 *   → 成员状态条（当前发言者放大）
 *   → 消息流（成员气泡 / 用户气泡 / 任务卡 / 文件卡）
 *   → 输入板 + 审批托盘（同一块玻璃容器）
 *
 * 三条纪律：
 *   - 黑白灰打底，彩色只给头像和状态语义
 *   - 输入板是一整块，不做「框里套框」
 *   - 审批托盘锚定输入板上方，与输入框同一个容器
 */
export default function Room() {
  const t = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const userId = useApp((s) => s.user?.id);
  const officialHints = useApp((s) => s.officialHints);

  const [conv, setConv] = useState<Conversation | null>(null);
  /** 这个群正在做的目标，作为群公告展示。取最新的进行中目标。 */
  const [goal, setGoal] = useState<Goal | null>(null);
  const [msgs, setMsgs] = useState<Message[]>([]);
  const [activity, setActivity] = useState<AgentActivity[]>([]);
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState('');
  const inputRef = useRef<TextInput>(null);
  const [err, setErr] = useState('');
  const [panelAgent, setPanelAgent] = useState<string | null>(null);
  const [infoOpen, setInfoOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [mentionOpen, setMentionOpen] = useState(false);
  const [approval, setApproval] = useState<{ a: Approval; runId: string } | null>(null);
  const [speaker, setSpeaker] = useState<string | null>(null);

  const abortRef = useRef<(() => void) | null>(null);
  const listRef = useRef<FlatList<Message>>(null);
  const streamingRef = useRef<Record<string, string>>({});
  const seenApprovals = useRef<Set<string>>(new Set());

  /* ---------- 成员 ---------- */
  const rawMembers: { key: string; name: string; title?: string; color?: string }[] =
    conv?.agents?.length
      ? conv.agents.map((a) => ({ key: a.key, name: a.name, title: a.title, color: a.color }))
      : activity.map((a) => ({ key: a.key, name: a.name }));

  const members: BarMember[] = rawMembers.map((m, i) => ({
    key: m.key,
    name: m.name,
    title: m.title,
    color: m.color,
    index: i,
    state: stateOf(activity.find((x) => x.key === m.key)),
  }));

  const workingCount = activity.filter((a) => a.state === 'working').length;
  const blockedCount = activity.filter((a) => a.state === 'blocked').length;

  /* ---------- 加载 ---------- */
  const loadConv = useCallback(async () => {
    try {
      const c = await get<Conversation>(`/v1/conversations/${id}`);
      setConv(c);
      const m = await get<{ messages?: Message[] } | Message[]>(
        `/v1/conversations/${id}/messages?page=1&limit=60`,
      );
      setMsgs(Array.isArray(m) ? m : (m?.messages ?? []));
    } catch (e) {
      setErr(humanError(e));
    }
  }, [id]);

  /**
   * 一轮轮询同时拿两样东西：角色状态 + 待审批。
   *
   * 之前是两个独立的 setInterval（5s 和 4s），加起来每分钟 ~27 次请求，
   * 再叠加消息刷新很容易撞上每用户 120/分钟的限流——表现出来就是
   * 「操作太频繁了」把整个消息列表盖掉。合并成一个就减半了。
   */
  const loadActivity = useCallback(async () => {
    try {
      const a = await get<AgentActivity[]>(`/v1/conversations/${id}/activity`);
      const list = Array.isArray(a) ? a : [];
      setActivity(list);

      // 有待审批的角色才去查 Run，没有就不发请求
      const blocked = list.filter((x) => x.state === 'blocked' && x.runId);
      if (blocked.length === 0) return;

      for (const b of blocked) {
        const run = await get<any>(`/v1/runs/${b.runId}`);
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
      /* 状态环/审批失败不影响聊天本身 */
    }
  }, [id]);

  useEffect(() => {
    loadConv();
    loadActivity();
    // 群公告取这个群最新的目标。拉不到就保持空，不编一个标题。
    get<Goal[]>('/v1/goals')
      .then((list) => {
        const mine = (Array.isArray(list) ? list : []).filter((g) => g.conversationId === id);
        mine.sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : -1));
        setGoal(mine.find((g) => g.status === 'doing') ?? mine[0] ?? null);
      })
      .catch(() => {});
  }, [loadConv, loadActivity, id]);

  // 一个 interval 管两件事：状态环 + 审批
  useEffect(() => {
    const timer = setInterval(loadActivity, 6000);
    return () => clearInterval(timer);
  }, [loadActivity]);


  useEffect(() => () => abortRef.current?.(), []);

  /* ---------- 发送 ---------- */
  async function send() {
    const text = input.trim();
    if (!text || busy) return;
    setInput('');
    setErr('');
    setBusy(true);
    setMentionOpen(false);
    streamingRef.current = {};

    setMsgs((m) => [
      ...m,
      {
        id: `tmp_${Date.now()}`,
        conversationId: id,
        role: 'user',
        content: text,
        tokensIn: 0,
        tokensOut: 0,
        latencyMs: 0,
        createdAt: new Date().toISOString(),
      },
    ]);
    setTimeout(() => listRef.current?.scrollToEnd({ animated: true }), 60);

    abortRef.current = await stream(`/v1/conversations/${id}/orchestrate`, { content: text }, handleEvent);
  }

  function handleEvent(ev: ChatEvent) {
    switch (ev.type) {
      case 'start':
        streamingRef.current[ev.agent ?? ''] = '';
        setSpeaker(ev.agent ?? null);
        setMsgs((m) => [
          ...m,
          {
            id: `live_${ev.agent}_${Date.now()}`,
            conversationId: id,
            role: 'assistant',
            agent: ev.name,
            content: '',
            tokensIn: 0,
            tokensOut: 0,
            latencyMs: 0,
            createdAt: new Date().toISOString(),
          },
        ]);
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
            conversationId: id,
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
        setSpeaker(null);
        loadConv();
        loadActivity();
        break;
    }
  }

  function stop() {
    abortRef.current?.();
    abortRef.current = null;
    setBusy(false);
    setSpeaker(null);
  }

  async function decide(approve: boolean, scope: 'once' | 'run') {
    if (!approval) return;
    const { a, runId } = approval;
    setApproval(null);
    try {
      await post(`/v1/runs/${runId}/approve`, { approvalId: a.id, approve, scope });
      loadActivity();
    } catch (e) {
      setErr(humanError(e));
    }
  }

  function insertMention(name: string) {
    setInput((v) => (v.endsWith(' ') || v === '' ? `${v}@${name} ` : `${v} @${name} `));
    setMentionOpen(false);
  }

  /* ---------- 状态句 ---------- */
  const isOfficial = conv?.studio === 'official';
  const statusLine =
    isOfficial
      ? '官方群 · 大家都在'
      : workingCount > 0 && blockedCount > 0
      ? `${workingCount} 位在干活 · ${blockedCount} 位等你审批`
      : workingCount > 0
        ? `${workingCount} 位在干活`
        : blockedCount > 0
          ? `${blockedCount} 位等你审批`
          : `${members.length} 位成员`;

  /**
   * 成员条文案。
   *
   * 设计图是「**撰写** 刚发了言 · **查证** 干活中」——名字加粗 + 灰色动作，
   * 按「刚发言 → 干活中 → 等审批 → 空闲」的优先级挑最多两个来说。
   * 之前只显示「都在待命」，等于把这个组件的信息量浪费了。
   */
  const barSummary = (() => {
    const nameOfKey = (k: string) => rawMembers.find((m) => m.key === k)?.name ?? '';

    // 有人在说话/干活时，状态优先——官方群的介绍文案只在没人动的时候显示
    const isQuiet = !speaker && activity.every((a) => a.state === 'idle');
    if (isOfficial && isQuiet) {
      return (
        <T size={type.caption} color={t.ink3} numberOfLines={1}>
          <Text style={{ color: t.ink, fontWeight: weight.heavy as any }}>团团</Text>
          <Text> 正在为大家介绍纸团</Text>
        </T>
      );
    }

    // 1) 正在发言的人：说「刚发了言」
    if (speaker) {
      const who = nameOfKey(speaker);
      const others = activity
        .filter((a) => a.state === 'working' && a.key !== speaker)
        .slice(0, 1)
        .map((a) => nameOfKey(a.key))
        .filter(Boolean);
      return (
        <T size={type.caption} color={t.ink3} numberOfLines={1}>
          <Text style={{ color: t.ink, fontWeight: weight.heavy as any }}>{who}</Text>
          <Text> 刚发了言</Text>
          {others.length ? (
            <>
              <Text> · </Text>
              <Text style={{ color: t.ink, fontWeight: weight.heavy as any }}>{others[0]}</Text>
              <Text> 干活中</Text>
            </>
          ) : null}
        </T>
      );
    }

    // 2) 有人在干活
    const working = activity.filter((a) => a.state === 'working');
    if (working.length > 0) {
      const [a, b] = working.map((x) => nameOfKey(x.key)).filter(Boolean);
      return (
        <T size={type.caption} color={t.ink3} numberOfLines={1}>
          <Text style={{ color: t.ink, fontWeight: weight.heavy as any }}>{a}</Text>
          <Text> 干活中</Text>
          {b ? (
            <>
              <Text> · </Text>
              <Text style={{ color: t.ink, fontWeight: weight.heavy as any }}>{b}</Text>
              <Text> 也在忙</Text>
            </>
          ) : null}
        </T>
      );
    }

    // 3) 有人等审批
    const blocked = activity.filter((a) => a.state === 'blocked');
    if (blocked.length > 0) {
      const who = blocked.map((a) => nameOfKey(a.key)).filter(Boolean).slice(0, 2).join('、');
      return (
        <T size={type.caption} color={t.bad} numberOfLines={1}>
          <Text style={{ fontWeight: weight.heavy as any }}>{who}</Text>
          <Text> 等你审批</Text>
        </T>
      );
    }

    // 4) 都没事做
    return (
      <T size={type.caption} color={t.ink3} numberOfLines={1}>
        都在待命
      </T>
    );
  })();


  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: t.canvas[0] }} edges={['top', 'bottom']}>
      <ChatHeader
        title={`${conv?.title ?? '加载中'}${members.length ? `（${members.length}）` : ''}`}
        statusLine={statusLine}
        onBack={() => router.back()}
        onMenu={() => setInfoOpen(true)}
      />

      {/* 成员状态条 */}
      {members.length > 0 ? (
        <MemberBar
          members={members}
          speakerKey={speaker}
          summary={barSummary}
          onOpenInfo={() => setInfoOpen(true)}
        />
      ) : null}

      {err ? (
        <Pressable onPress={() => setErr('')} style={[s.errBar, { backgroundColor: t.badSoft }]}>
          <Icon name="alert-circle" size={14} color={t.bad} />
          <T size={type.caption} color={t.bad} style={{ flex: 1 }}>
            {err}
          </T>
        </Pressable>
      ) : null}

      {/* 消息流 */}
      <FlatList
        ref={listRef}
        data={msgs}
        keyExtractor={(m, i) => `${m.id}_${i}`}
        contentContainerStyle={s.list}
        showsVerticalScrollIndicator={false}
        renderItem={({ item, index }) => (
          <MessageRow
            msg={item}
            prev={msgs[index - 1]}
            members={rawMembers}
            streaming={busy && index === msgs.length - 1}
            onAvatar={(name) => {
              const found = rawMembers.find((x) => x.name === name);
              if (found) setPanelAgent(found.key);
            }}
            onOpenFile={(a) => (a?.id ? router.push(`/artifact/${a.id}`) : router.push('/library'))}
          />
        )}
        onContentSizeChange={() => {
          if (busy) listRef.current?.scrollToEnd({ animated: true });
        }}
      />

      {/* 建议 chips：官方群给新手引导语，其余群给常用指令。
          右侧渐隐表示「还能往右滑」——只露出半张卡又不给提示，
          看起来像是排版坏了。 */}
      {!busy && msgs.length > 0 ? (
        <View style={{ flexGrow: 0, flexShrink: 0 }}>
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={s.chipRow}
          >
            {(officialHints.length
              ? officialHints
              : ['帮我写一份周报提纲', '帮我整理会议纪要', '创建一个新团队']
            ).map((h) => (
              <Pressable
                key={h}
                onPress={() => setInput(h)}
                style={[s.chip, { backgroundColor: t.card, borderColor: t.hairline }]}
              >
                <Text style={{ fontSize: 12, color: t.ink, marginRight: 5 }}>✦</Text>
                <T size={type.caption} color={t.ink} weight={weight.medium} numberOfLines={1}>
                  {h}
                </T>
              </Pressable>
            ))}
          </ScrollView>
          <LinearGradient
            pointerEvents="none"
            colors={[withAlpha(t.canvas[0], 0), t.canvas[0], t.canvas[0]]}
            locations={[0, 0.55, 1]}
            start={{ x: 0, y: 0 }}
            end={{ x: 1, y: 0 }}
            style={{ position: 'absolute', right: 0, top: 0, bottom: 8, width: 44, zIndex: 2 }}
          />
        </View>
      ) : null}

      {/* 输入板 + 审批托盘：同一块容器 */}
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View
          style={[
            s.dock,
            {
              backgroundColor: t.slab,
              borderColor: t.hairline,
            },
          ]}
        >
          {/* 审批托盘：托盘与输入区连成一块，中间发丝线 */}
          {approval ? (
            <>
              <ApprovalTray
                approval={approval.a}
                level={rawMembers.find((m) => m.key === (approval.a as any).agentKey)?.title}
                onAllow={() => decide(true, 'once')}
                onDeny={() => decide(false, 'once')}
                onAllowRun={() => decide(true, 'run')}
                onOpenSettings={() => {
                  setApproval(null);
                  setInfoOpen(true);
                }}
              />
              <View style={[s.dockDivider, { backgroundColor: t.hairline }]} />
            </>
          ) : null}

          {/* 输入行：图标直接落在板上，输入框不描边不填底 */}
          <View style={s.inputRow}>
            <TextInput
              ref={inputRef}
              value={input}
              onChangeText={setInput}
              placeholder="发消息，或 @ 指定成员"
              placeholderTextColor={t.ink4}
              multiline
              style={[s.input, { color: t.ink }]}
              editable={!busy}
              // 桌面/Web 上回车＝发送。multiline 输入框不会触发
              // onSubmitEditing，只能从按键事件里拦。
              // 用输入法组合中的回车（选词）不算发送。
              onKeyPress={(e: any) => {
                const ne = e?.nativeEvent ?? {};
                if (ne.key !== 'Enter' || ne.shiftKey || ne.isComposing) return;
                if (!input.trim() || busy) return;
                e.preventDefault?.();
                void send();
              }}
            />
            <Pressable onPress={() => setMentionOpen((v) => !v)} hitSlop={8} style={s.iconBtn}>
              <Icon name="at" size={21} color={mentionOpen ? t.ink : t.ink3} />
            </Pressable>
            {busy ? (
              <Pressable onPress={stop} hitSlop={8} style={s.iconBtn}>
                <View style={[s.stopSquare, { backgroundColor: t.ink }]} />
              </Pressable>
            ) : (
              <Pressable
                onPress={input.trim() ? send : undefined}
                hitSlop={8}
                style={s.iconBtn}
                disabled={!input.trim()}
              >
                <Icon
                  name={input.trim() ? 'arrow-up-circle' : 'add-circle-outline'}
                  size={24}
                  color={input.trim() ? t.ink : t.ink3}
                />
              </Pressable>
            )}
          </View>

          {mentionOpen ? (
            <MentionPicker members={rawMembers} onPick={insertMention} onClose={() => setMentionOpen(false)} />
          ) : null}
        </View>
      </KeyboardAvoidingView>

      <AddMemberSheet
        visible={addOpen}
        conv={conv}
        onClose={() => setAddOpen(false)}
        onAdded={(next) => {
          setConv(next);
          setAddOpen(false);
          loadConv();
        }}
      />

      <AgentPanel
        visible={!!panelAgent}
        agent={(() => {
          const f = rawMembers.find((a) => a.key === panelAgent);
          return f ? { key: f.key, name: f.name, title: f.title, color: f.color } : null;
        })()}
        activity={activity.find((a) => a.key === panelAgent)}
        convId={id}
        workspaceId={(conv as any)?.workspaceId}
        onClose={() => setPanelAgent(null)}
        onDirect={(name) => {
          // 关面板 → 输入框里预先 @ 他：用户下一步一定是打字，
          // 不把他带回输入框等于让他自己找一遍。
          setPanelAgent(null);
          insertMention(name);
          inputRef.current?.focus();
        }}
      />

      <GroupInfoPanel
        visible={infoOpen}
        conv={conv}
        members={rawMembers.map((m, i) => ({
          key: m.key,
          name: m.name,
          title: m.title,
          color: m.color ?? memberColor(i),
          index: i,
          state: stateOf(activity.find((a) => a.key === m.key)),
        }))}
        activity={activity}
        goal={goal ? { title: goal.title, status: goal.status } : null}
        isOwner={conv?.userId === userId}
        onClose={() => setInfoOpen(false)}
        onOpenMember={(key) => {
          setInfoOpen(false);
          setPanelAgent(key);
        }}
        onAddMember={() => {
          setInfoOpen(false);
          setAddOpen(true);
        }}
        onRename={async (title: string) => {
          // 必须写回服务端：面板会在 await 成功后才显示新名字，
          // 只改本地状态等于骗用户「改名成功了」。
          const next = await patch<Conversation>(`/v1/conversations/${id}`, { title });
          setConv((c) => (c ? { ...c, title: next?.title ?? title } : c));
        }}
        onSetPermission={async (level: string) => {
          const next = await patch<Conversation>(`/v1/conversations/${id}`, {
            defaultPermission: level,
          });
          setConv((c) =>
            c ? { ...c, defaultPermission: next?.defaultPermission ?? level } : c,
          );
        }}
        onLeave={() => {
          Alert.alert('退出团队', `退出「${conv?.title ?? ''}」后将不再收到它的消息。`, [
            { text: '取消', style: 'cancel' },
            {
              text: '退出',
              style: 'destructive',
              onPress: async () => {
                try {
                  await post(`/v1/conversations/${id}/leave`, {});
                } catch (e) {
                  Alert.alert('退出失败', humanError(e));
                  return;
                }
                router.replace('/(main)');
              },
            },
          ]);
        }}
        onOpenLibrary={() => router.push('/library')}
        onOpenGoals={() => router.push('/goals')}
        onOpenConnect={() => router.push('/connect')}
        onOpenTask={() => {
          setInfoOpen(false);
          router.push(`/team/task?convId=${id}`);
        }}
      />
    </SafeAreaView>
  );
}

/* ============================================================
   消息行
   ============================================================ */

function MessageRow({
  msg,
  prev,
  members,
  streaming,
  onAvatar,
  onOpenFile,
}: {
  msg: Message;
  prev?: Message;
  members: { key: string; name: string; color?: string }[];
  streaming: boolean;
  onAvatar: (name: string) => void;
  onOpenFile: (a?: ArtifactRef) => void;
}) {
  const t = useTheme();

  // 非文本消息（system 角色的富卡片）
  if (msg.role === 'system' && msg.artifacts?.length) {
    const title = msg.artifacts[0];
    // 这里只有「产出了什么」，没有进度也没有审批状态。
    // 以前写死 step=3/5「等你审批」，等于给每条产物消息编一个假状态。
    const ref = msg.attachments?.[0];
    return (
      <View style={{ marginVertical: 5, marginLeft: 50 }}>
        <FileCard
          name={title}
          meta={msg.agent ? `${msg.agent} 产出` : '产出'}
          onPress={() => onOpenFile(ref)}
        />
      </View>
    );
  }

  if (msg.role === 'system') {
    return (
      <View style={{ alignItems: 'center', paddingVertical: 6 }}>
        <T size={type.micro} color={t.faint}>
          {msg.content}
        </T>
      </View>
    );
  }

  if (msg.role === 'user') {
    return (
      <View style={{ marginVertical: 5, paddingLeft: 50 }}>
        <UserBubble>
          <T size={type.body} lh={lead.body} color={t.dark ? t.primaryInk : '#FFFFFF'}>
            {msg.content}
          </T>
        </UserBubble>
      </View>
    );
  }

  /* ---------- 成员消息 ---------- */
  const showHead = prev?.agent !== msg.agent;
  const idx = members.findIndex((m) => m.name === msg.agent);
  const color = idx >= 0 ? (members[idx].color ?? memberColor(idx)) : undefined;

  // 能力卡：标题 + 三条要点（设计图 v19 第三屏）
  const cap = (msg as any).capability;
  // 任务卡：标题 + 状态 + 内嵌文件（设计图 v19 第四屏）
  const task = (msg as any).taskCard;
  // 名字后的徽章：官方/主持
  const badge = (msg as any).badge;

  return (
    <View style={{ marginTop: showHead ? 11 : 4 }}>
      <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: 10 }}>
        <Pressable onPress={() => onAvatar(msg.agent ?? '')} style={{ width: 40 }}>
          {showHead ? (
            <Avatar name={msg.agent ?? '?'} size={40} index={idx >= 0 ? idx : 0} tint={color} />
          ) : null}
        </Pressable>
        <View style={{ flex: 1, maxWidth: '86%' }}>
          {showHead ? (
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, marginBottom: 5 }}>
              <T size={type.micro} color={t.ink3} weight={weight.semibold}>
                {msg.agent}
              </T>
              {badge ? (
                <View
                  style={[
                    s.badge,
                    {
                      backgroundColor: badge === 'official' ? t.goldBg : t.cardSunken,
                      borderColor: badge === 'official' ? t.goldFg + '33' : t.hairline,
                    },
                  ]}
                >
                  <T
                    size={9.5}
                    weight={weight.semibold}
                    color={badge === 'official' ? t.goldFg : t.ink3}
                  >
                    {badge === 'official' ? '官方' : '主持'}
                  </T>
                </View>
              ) : null}
            </View>
          ) : null}

          {cap ? (
            <CapabilityBubble card={cap} />
          ) : task ? (
            <TaskMessageCard task={task} onOpenFile={() => {}} />
          ) : (
            <MemberBubble>
              {msg.content ? (
                <T size={type.body} lh={lead.body}>
                  {msg.content}
                </T>
              ) : streaming ? (
                <TypingDots color={t.ink4} />
              ) : (
                <T size={type.body} color={t.ink4}>
                  …
                </T>
              )}
            </MemberBubble>
          )}
        </View>
      </View>
    </View>
  );
}

/* ============================================================
   富消息卡片
   ============================================================ */

/** 能力卡：标题 + 要点列表（设计图「纸团能帮你做什么」）。 */
function CapabilityBubble({
  card,
}: {
  card: { title: string; items: { title: string; desc: string }[] };
}) {
  const t = useTheme();
  return (
    <View style={[s.capCard, { backgroundColor: t.card, borderColor: t.hairline }]}>
      <T size={type.card} weight={weight.strong} style={{ marginBottom: 10 }}>
        {card.title}
      </T>
      {card.items.map((it, i) => (
        <View key={it.title} style={{ flexDirection: 'row', gap: 9, marginTop: i === 0 ? 0 : 9 }}>
          <View style={[s.capDot, { backgroundColor: t.ink }]} />
          <View style={{ flex: 1 }}>
            <T size={type.sub} weight={weight.strong} lh={20}>
              {it.title}
            </T>
            <T size={type.caption} color={t.ink3} lh={19} style={{ marginTop: 1 }}>
              {it.desc}
            </T>
          </View>
        </View>
      ))}
    </View>
  );
}

/** 任务卡：类型行 + 标题 + 内嵌文件行。 */
function TaskMessageCard({
  task,
  onOpenFile,
}: {
  task: {
    title: string;
    status: string;
    statusText?: string;
    file?: { name: string; path?: string };
  };
  onOpenFile: () => void;
}) {
  const t = useTheme();
  const tone = task.status === 'done' ? 'done' : task.status === 'blocked' ? 'blocked' : 'working';
  return (
    <View style={[s.taskCard, { backgroundColor: t.card, borderColor: t.hairline }]}>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
          <Icon name="checkbox-outline" size={13} color={t.ink2} />
          <T size={type.micro} color={t.ink2} weight={weight.semibold}>
            任务
          </T>
        </View>
        <View
          style={[
            s.statusPill,
            {
              backgroundColor:
                tone === 'done' ? t.okSoft : tone === 'blocked' ? t.badSoft : t.accentSoft,
            },
          ]}
        >
          <T
            size={type.micro}
            weight={weight.semibold}
            color={tone === 'done' ? t.ok : tone === 'blocked' ? t.bad : t.accentText}
          >
            {task.statusText ?? (tone === 'done' ? '已完成' : tone === 'blocked' ? '等你审批' : '执行中')}
          </T>
        </View>
      </View>
      <T size={type.card} weight={weight.strong} lh={21} style={{ marginTop: 9 }}>
        {task.title}
      </T>
      {task.file ? (
        <Pressable
          onPress={onOpenFile}
          style={[s.taskFileRow, { backgroundColor: t.cardSunken }]}
        >
          <View style={[s.taskFileIcon, { backgroundColor: withAlpha('#7C93A8', 0.16) }]}>
            <Icon name="document-text" size={17} color="#7C93A8" />
          </View>
          <View style={{ flex: 1 }}>
            <T size={type.sub} weight={weight.strong} numberOfLines={1}>
              {task.file.name}
            </T>
            <T size={type.micro} color={t.ink3} style={{ marginTop: 2 }}>
              已存入资料库 · 点开就能看
            </T>
          </View>
        </Pressable>
      ) : null}
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
  return (
    <View style={[s.mention, { backgroundColor: t.card, borderColor: t.hairline }]}>
      <View style={{ flexDirection: 'row', justifyContent: 'space-between', marginBottom: 8 }}>
        <T size={type.micro} color={t.ink3} weight={weight.semibold}>
          指定成员
        </T>
        <Pressable onPress={onClose} hitSlop={8}>
          <Icon name="close" size={15} color={t.ink3} />
        </Pressable>
      </View>
      <ScrollView style={{ maxHeight: 180 }}>
        {members.map((m, i) => (
          <Pressable key={m.key} onPress={() => onPick(m.name)} style={{ paddingVertical: 8 }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
              <Avatar name={m.name} size={30} index={i} tint={m.color} />
              <View style={{ flex: 1 }}>
                <T size={type.sub} weight={weight.semibold}>
                  {m.name}
                </T>
                {m.title ? (
                  <T size={type.micro} color={t.ink3} style={{ marginTop: 1 }}>
                    {m.title}
                  </T>
                ) : null}
              </View>
            </View>
          </Pressable>
        ))}
      </ScrollView>
    </View>
  );
}

/* ============================================================
   工具
   ============================================================ */

function stateOf(a?: AgentActivity): MemberState {
  if (!a) return 'idle';
  if (a.state === 'working') return 'working';
  if (a.state === 'done') return 'done';
  if (a.state === 'blocked') return 'blocked';
  return 'idle';
}

const s = StyleSheet.create({
  list: { paddingHorizontal: 12, paddingTop: 10, paddingBottom: 16 },

  errBar: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 7,
    marginHorizontal: 12,
    marginBottom: 8,
    paddingVertical: 9,
    paddingHorizontal: 12,
    borderRadius: radius.tag,
  },

  chipRow: {
    paddingHorizontal: 12,
    gap: 8,
    paddingBottom: 8,
  },
  chip: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 13,
    paddingVertical: 8,
    borderRadius: radius.pill,
    borderWidth: StyleSheet.hairlineWidth,
  },
  dock: {
    marginHorizontal: 12,
    marginBottom: 8,
    borderRadius: radius.tray,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
    ...shadow.tray,
  },
  dockDivider: { height: StyleSheet.hairlineWidth, marginHorizontal: 16 },

  inputRow: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    paddingHorizontal: 10,
    paddingVertical: 8,
    minHeight: 56,
  },
  iconBtn: {
    width: 40,
    height: 40,
    alignItems: 'center',
    justifyContent: 'center',
  },
  input: {
    flex: 1,
    minHeight: 40,
    maxHeight: 120,
    paddingTop: 9,
    paddingBottom: 9,
    paddingHorizontal: 2,
    fontSize: type.body,
  },
  stopSquare: { width: 12, height: 12, borderRadius: 3 },

  badge: {
    paddingHorizontal: 5,
    paddingVertical: 1.5,
    borderRadius: 5,
    borderWidth: StyleSheet.hairlineWidth,
  },
  capCard: {
    borderRadius: radius.message,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 14,
    ...shadow.card,
  },
  capDot: { width: 5, height: 5, borderRadius: 3, marginTop: 7 },
  taskCard: {
    borderRadius: radius.message,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 13,
    ...shadow.card,
  },
  statusPill: { paddingHorizontal: 8, paddingVertical: 3, borderRadius: radius.pill },
  taskFileRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    borderRadius: 12,
    padding: 10,
    marginTop: 11,
  },
  taskFileIcon: {
    width: 36,
    height: 36,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  mention: {
    marginHorizontal: 10,
    marginBottom: 10,
    borderRadius: radius.tag,
    padding: 12,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
