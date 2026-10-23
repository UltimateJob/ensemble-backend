import { Ionicons } from '@expo/vector-icons';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
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
import { AgentPanel } from '../../src/components/AgentPanel';
import { ApprovalTray } from '../../src/components/ApprovalTray';
import { Avatar } from '../../src/components/Avatar';
import { GroupInfoPanel } from '../../src/components/GroupInfoPanel';
import {
  ChatHeader,
  FileCard,
  MemberBar,
  MemberBubble,
  TaskCard,
  UserBubble,
  memberColor,
  withAlpha,
  type BarMember,
  type MemberState,
} from '../../src/components/chat';
import { TypingDots } from '../../src/components/motion';
import { Icon, T } from '../../src/components/ui';
import { get, humanError, post, stream } from '../../src/lib/api';
import type { Approval } from '../../src/lib/runs';
import { useApp } from '../../src/lib/store';
import type { AgentActivity, ChatEvent, Conversation, Message } from '../../src/lib/types';
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

  const [conv, setConv] = useState<Conversation | null>(null);
  const [msgs, setMsgs] = useState<Message[]>([]);
  const [activity, setActivity] = useState<AgentActivity[]>([]);
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState('');
  const [err, setErr] = useState('');
  const [panelAgent, setPanelAgent] = useState<string | null>(null);
  const [infoOpen, setInfoOpen] = useState(false);
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

  const loadActivity = useCallback(async () => {
    try {
      const a = await get<AgentActivity[]>(`/v1/conversations/${id}/activity`);
      setActivity(Array.isArray(a) ? a : []);
    } catch {
      /* 状态环失败不影响聊天 */
    }
  }, [id]);

  useEffect(() => {
    loadConv();
    loadActivity();
  }, [loadConv, loadActivity]);

  useEffect(() => {
    const timer = setInterval(loadActivity, 5000);
    return () => clearInterval(timer);
  }, [loadActivity]);

  // 审批轮询
  useEffect(() => {
    let alive = true;
    async function poll() {
      try {
        const acts = await get<AgentActivity[]>(`/v1/conversations/${id}/activity`);
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
        /* 下一轮 */
      }
    }
    const timer = setInterval(poll, 4000);
    poll();
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [id]);

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
  const statusLine =
    workingCount > 0 && blockedCount > 0
      ? `${workingCount} 位在干活 · ${blockedCount} 位等你审批`
      : workingCount > 0
        ? `${workingCount} 位在干活`
        : blockedCount > 0
          ? `${blockedCount} 位等你审批`
          : `${members.length} 位成员`;

  const barSummary = speaker ? (
    <T size={type.caption} color={t.ink} numberOfLines={1}>
      <Text style={{ fontWeight: weight.heavy as any }}>{rawMembers.find((m) => m.key === speaker)?.name}</Text>
      <Text style={{ color: t.ink3 }}> 正在发言</Text>
    </T>
  ) : (
    <T size={type.caption} color={t.ink3} numberOfLines={1}>
      {workingCount > 0 ? `${workingCount} 位在干活` : '都在待命'}
    </T>
  );

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: t.canvas[0] }} edges={['top', 'bottom']}>
      <ChatHeader
        title={conv?.title ?? '加载中'}
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
          />
        )}
        onContentSizeChange={() => {
          if (busy) listRef.current?.scrollToEnd({ animated: true });
        }}
      />

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
            <Pressable hitSlop={8} style={s.iconBtn}>
              <Icon name="mic-outline" size={22} color={t.ink3} />
            </Pressable>
            <TextInput
              value={input}
              onChangeText={setInput}
              placeholder="发消息，或 @ 指定成员"
              placeholderTextColor={t.ink4}
              multiline
              style={[s.input, { color: t.ink }]}
              editable={!busy}
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
        goal={null}
        muted={false}
        isOwner={conv?.userId === userId}
        onClose={() => setInfoOpen(false)}
        onOpenMember={(key) => {
          setInfoOpen(false);
          setPanelAgent(key);
        }}
        onAddMember={() => {}}
        onRename={async () => {}}
        onSetPermission={async () => {}}
        onToggleMute={() => {}}
        onLeave={() => router.back()}
        onOpenLibrary={() => router.push('/library')}
        onOpenGoals={() => router.push('/goals')}
        onOpenConnect={() => router.push('/connect')}
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
}: {
  msg: Message;
  prev?: Message;
  members: { key: string; name: string; color?: string }[];
  streaming: boolean;
  onAvatar: (name: string) => void;
}) {
  const t = useTheme();

  // 任务卡 / 文件卡
  if (msg.role === 'system' && msg.artifacts?.length) {
    const title = msg.artifacts[0];
    const isTask = /任务|方案|架构/.test(title);
    return (
      <View style={{ marginVertical: 5, marginLeft: 50 }}>
        {isTask ? (
          <TaskCard title={title} step={3} total={5} status="等你审批" statusTone="blocked" />
        ) : (
          <FileCard name={title} meta={msg.agent ?? ''} />
        )}
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

  // 成员消息
  const showHead = prev?.agent !== msg.agent;
  const idx = members.findIndex((m) => m.name === msg.agent);
  const color = idx >= 0 ? (members[idx].color ?? memberColor(idx)) : undefined;

  return (
    <View style={{ marginTop: showHead ? 11 : 4 }}>
      <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: 10 }}>
        <Pressable onPress={() => onAvatar(msg.agent ?? '')} style={{ width: 40 }}>
          {showHead ? <Avatar name={msg.agent ?? '?'} size={40} index={idx >= 0 ? idx : 0} tint={color} /> : null}
        </Pressable>
        <View style={{ flex: 1, maxWidth: '84%' }}>
          {showHead ? (
            <T size={type.micro} color={t.ink3} weight={weight.semibold} style={{ marginBottom: 5 }}>
              {msg.agent}
            </T>
          ) : null}
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
        </View>
      </View>
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

  mention: {
    marginHorizontal: 10,
    marginBottom: 10,
    borderRadius: radius.tag,
    padding: 12,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
