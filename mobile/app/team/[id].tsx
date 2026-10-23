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
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AgentPanel } from '../../src/components/AgentPanel';
import { ApprovalSheet } from '../../src/components/ApprovalSheet';
import { Avatar } from '../../src/components/Avatar';
import { LinearGradient } from 'expo-linear-gradient';
import { Canvas } from '../../src/components/Canvas';
import { FadeInUp, PressableScale, TypingDots } from '../../src/components/motion';
import { Card, Icon, IconButton, PulseDot, Row, T, withAlpha } from '../../src/components/ui';
import { get, humanError, post, stream } from '../../src/lib/api';
import type { Approval } from '../../src/lib/runs';
import type { AgentActivity, ChatEvent, Conversation, Message } from '../../src/lib/types';
import { color, gap, leading, PAGE_X, radius, shadow, type, weight } from '../../src/theme/tokens';

/**
 * 群聊房间。
 *
 * 视觉结构：
 *   顶栏（返回 / 团队名 / 设置）
 *   → 成员条：头像 + 名字，状态环只在"有事发生"时出现
 *   → 状态条：只在有人后台干活时出现，是"谁在动"的主信号
 *   → 消息流：用户气泡靠右（主色实底），Agent 靠左（灰底白字区块）
 *   → 输入栏：圆角输入框 + 主色发送键
 *
 * Agent 静默是默认行为：只在协商和同步结论时说话，后台干活靠头像环表达。
 */
export default function Room() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();

  const [conv, setConv] = useState<Conversation | null>(null);
  const [msgs, setMsgs] = useState<Message[]>([]);
  const [activity, setActivity] = useState<AgentActivity[]>([]);
  const [busy, setBusy] = useState(false);
  const [input, setInput] = useState('');
  const [err, setErr] = useState('');
  const [panelAgent, setPanelAgent] = useState<string | null>(null);
  const [pendingApproval, setPendingApproval] = useState<{
    approval: Approval;
    runId: string;
  } | null>(null);

  const abortRef = useRef<(() => void) | null>(null);
  const listRef = useRef<FlatList<Message>>(null);
  const streamingRef = useRef<Record<string, string>>({});
  const seenApprovals = useRef<Set<string>>(new Set());

  /**
   * 成员来源。
   *
   * 内置场景的群，会话详情里没有 agents（角色是运行时由场景解析的）。
   * /activity 会带上解析后的 key/name，用它作为主来源。
   */
  const members: { key: string; name: string; title?: string; color?: string }[] =
    conv?.agents?.length
      ? conv.agents.map((a) => ({ key: a.key, name: a.name, title: a.title, color: a.color }))
      : activity.map((a) => ({
          key: a.key,
          name: a.name,
          color: (conv?.agents ?? []).find((x) => x.key === a.key)?.color,
        }));

  const loadConv = useCallback(async () => {
    try {
      const c = await get<Conversation>(`/v1/conversations/${id}`);
      setConv(c);
      const m = await get<{ messages?: Message[] } | Message[]>(
        `/v1/conversations/${id}/messages?page=1&limit=40`,
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
    const t = setInterval(loadActivity, 5000);
    return () => clearInterval(t);
  }, [loadActivity]);

  // 审批轮询：低频事件，4 秒一次比给每个 Run 常驻 SSE 更省电
  useEffect(() => {
    let alive = true;
    async function poll() {
      try {
        const acts = await get<AgentActivity[]>(`/v1/conversations/${id}/activity`);
        if (!alive) return;
        const blocked = acts.filter((a) => a.state === 'blocked' && a.runId);
        for (const b of blocked) {
          const run = await get<any>(`/v1/runs/${b.runId}`);
          if (!alive) return;
          const pending = (run?.approvals ?? []).find(
            (x: any) => x.status === 'pending' && !seenApprovals.current.has(x.id),
          );
          if (pending) {
            seenApprovals.current.add(pending.id);
            setPendingApproval({ approval: pending, runId: b.runId! });
            break;
          }
        }
      } catch {
        /* 下一轮再试 */
      }
    }
    const t = setInterval(poll, 4000);
    poll();
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [id]);

  useEffect(() => () => abortRef.current?.(), []);

  async function send() {
    const text = input.trim();
    if (!text || busy) return;
    setInput('');
    setErr('');
    setBusy(true);
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

    abortRef.current = await stream(`/v1/conversations/${id}/orchestrate`, { content: text }, (ev: ChatEvent) =>
      handleEvent(ev),
    );
  }

  function handleEvent(ev: ChatEvent) {
    switch (ev.type) {
      case 'start': {
        streamingRef.current[ev.agent ?? ''] = '';
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
        setActivity((a) =>
          a.map((x) => (x.key === ev.agent ? { ...x, state: 'working', detail: '正在发言' } : x)),
        );
        break;
      }
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
        setActivity((a) =>
          a.map((x) =>
            x.key === ev.agent ? { ...x, state: x.state === 'working' ? 'idle' : x.state } : x,
          ),
        );
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
      case 'ask':
      case 'error':
        if (ev.content) {
          setMsgs((m) => [
            ...m,
            {
              id: `sys_${Date.now()}_${Math.random()}`,
              conversationId: id,
              role: 'system',
              content: ev.content!,
              tokensIn: 0,
              tokensOut: 0,
              latencyMs: 0,
              createdAt: new Date().toISOString(),
            },
          ]);
        }
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
        loadConv();
        loadActivity();
        break;
    }
  }

  function stop() {
    abortRef.current?.();
    abortRef.current = null;
    setBusy(false);
  }

  async function decide(approve: boolean, scope: 'once' | 'run') {
    if (!pendingApproval) return;
    const { approval, runId } = pendingApproval;
    setPendingApproval(null);
    try {
      await post(`/v1/runs/${runId}/approve`, { approvalId: approval.id, approve, scope });
      loadActivity();
    } catch (e) {
      setErr(humanError(e));
    }
  }

  const working = activity.filter((a) => a.state === 'working' || a.state === 'blocked');
  const canSend = !!input.trim() && !busy;

  return (
    <Canvas>
    <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
      {/* 顶栏 */}
      <View style={s.topbar}>
        <IconButton name="chevron-back" onPress={() => router.back()} accessibilityLabel="返回" />
        <View style={s.topCenter}>
          <T size={type.headline} weight="600" numberOfLines={1}>
            {conv?.title ?? '加载中'}
          </T>
          {members.length ? (
            <T size={type.caption} color={color.ink3} style={{ marginTop: 1 }}>
              {members.length} 位成员
            </T>
          ) : null}
        </View>
        <IconButton
          name="ellipsis-horizontal"
          onPress={() => router.push('/settings')}
          accessibilityLabel="团队设置"
        />
      </View>

      {/* 成员条 */}
      <View style={s.membersWrap}>
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={s.membersRow}
        >
          {members.map((a) => {
            const st = activity.find((x) => x.key === a.key);
            return (
              <Pressable
                key={a.key}
                onPress={() => setPanelAgent(a.key)}
                style={({ pressed }) => [s.memberItem, pressed && { opacity: 0.7 }]}
              >
                <Avatar name={a.name} tint={a.color} size={46} state={st?.state} />
                <T
size={type.caption}
                  color={st?.state === 'working' ? color.accent : color.ink2}
                  weight={st?.state === 'working' ? '600' : '400'}
                  numberOfLines={1}
                  style={{ marginTop: 7 }}
                >
                  {a.name}
                </T>
              </Pressable>
            );
          })}
        </ScrollView>
      </View>

      {/* 状态条：只在有人干活时出现 */}
      {working.length > 0 ? (
        <View style={s.banner}>
          <PulseDot active tone={color.accent} />
          <T size={type.sub} color={color.accent} style={{ flex: 1 }} numberOfLines={2}>
            {working.some((a) => a.state === 'blocked')
              ? `${working.map((a) => a.name).join('、')} 在等你处理`
              : `${working.map((a) => a.name).join('、')} 正在后台处理`}
          </T>
        </View>
      ) : null}

      {err ? (
        <Pressable onPress={() => setErr('')} style={[s.banner, s.bannerErr]}>
          <Icon name="alert-circle-outline" size={16} color={color.bad} />
          <T size={type.sub} color={color.bad} style={{ flex: 1 }}>
            {err}
          </T>
        </Pressable>
      ) : null}

      {/* 消息流 */}
      <FlatList
        ref={listRef}
        data={msgs}
        keyExtractor={(m, i) => `${m.id}_${i}`}
        contentContainerStyle={s.msgList}
        showsVerticalScrollIndicator={false}
        renderItem={({ item, index }) => {
          const prev = msgs[index - 1];
          const showAgent = item.role === 'assistant' && prev?.agent !== item.agent;
          const isLast = index === msgs.length - 1;
          return <Bubble msg={item} showAgent={showAgent} conv={conv} streaming={busy && isLast} />;
        }}
        onContentSizeChange={() => {
          if (busy) listRef.current?.scrollToEnd({ animated: true });
        }}
      />

      {/* 输入栏 */}
      <KeyboardAvoidingView behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={s.composer}>
          <TextInput
            value={input}
            onChangeText={setInput}
            placeholder="@角色名字 发起任务"
            placeholderTextColor={color.ink4}
            multiline
            style={s.input}
            editable={!busy}
          />
          {busy ? (
            <Pressable onPress={stop} style={s.stopBtn} accessibilityLabel="停止">
              <View style={s.stopSquare} />
            </Pressable>
          ) : (
            <Pressable
              onPress={send}
              disabled={!canSend}
              style={[s.sendBtn, !canSend && s.sendBtnOff]}
              accessibilityLabel="发送"
            >
              <Ionicons name="arrow-up" size={20} color={color.white} />
            </Pressable>
          )}
        </View>
      </KeyboardAvoidingView>

      <AgentPanel
        visible={!!panelAgent}
        agent={(() => {
          const found = members.find((a) => a.key === panelAgent);
          return found
            ? { key: found.key, name: found.name, title: found.title, color: found.color }
            : null;
        })()}
        activity={activity.find((a) => a.key === panelAgent)}
        convId={id}
        workspaceId={(conv as any)?.workspaceId}
        onClose={() => setPanelAgent(null)}
      />

      <ApprovalSheet
        visible={!!pendingApproval}
        tool={pendingApproval?.approval.tool ?? ''}
        summary={pendingApproval?.approval.command || pendingApproval?.approval.summary || ''}
        risk={pendingApproval?.approval.risk ?? 'write'}
        reason={pendingApproval?.approval.summary}
        onAllow={() => decide(true, 'once')}
        onAllowRun={() => decide(true, 'run')}
        onDeny={() => decide(false, 'once')}
      />
    </SafeAreaView>
    </Canvas>
  );
}

/* ---------------- 气泡 ---------------- */

/** LinearGradient 需要至少两个色标的元组类型。 */
const grad = (c: readonly string[]) => c as unknown as readonly [string, string, ...string[]];

function Bubble({
  msg,
  showAgent,
  conv,
  streaming,
}: {
  msg: Message;
  showAgent: boolean;
  conv: Conversation | null;
  streaming: boolean;
}) {
  if (msg.role === 'system' && msg.artifacts?.length) return <TaskCard msg={msg} />;

  if (msg.role === 'system') {
    return (
      <View style={{ alignItems: 'center', paddingVertical: gap.xxs }}>
        <T size={type.caption} color={color.ink3} align="center" lh={leading.caption}>
          {msg.content}
        </T>
      </View>
    );
  }

  if (msg.role === 'user') {
    return (
      <FadeInUp distance={10}>
      <View style={{ alignItems: 'flex-end', marginBottom: gap.sm }}>
        <LinearGradient
          colors={grad(color.gradient)}
          start={{ x: 0, y: 0 }}
          end={{ x: 1, y: 1 }}
          style={s.userBubble}
        >
          <T size={type.body} color={color.white} lh={leading.body}>
            {msg.content}
          </T>
        </LinearGradient>
      </View>
      </FadeInUp>
    );
  }

  const agent = conv?.agents?.find((a) => a.name === msg.agent);
  return (
    <View style={[s.agentRow, { marginTop: showAgent ? gap.md : gap.xxs }]}>
      <View style={{ width: 34 }}>
        {showAgent ? <Avatar name={msg.agent ?? '?'} size={34} tint={agent?.color} /> : null}
      </View>
      <View style={{ flex: 1, maxWidth: '84%' }}>
        {showAgent ? (
          <T size={type.caption} color={color.ink3} weight="500" style={{ marginBottom: 5 }}>
            {msg.agent}
          </T>
        ) : null}
        <View style={s.agentBubble}>
          {msg.content ? (
            <T size={type.body} lh={leading.body}>
              {msg.content}
            </T>
          ) : streaming ? (
            <TypingDots color={color.ink4} />
          ) : (
            <T size={type.body} color={color.ink4}>
              …
            </T>
          )}
        </View>
      </View>
    </View>
  );
}

/** 通栏任务卡：所有重要产出走这里，而不是大段刷屏气泡。 */
function TaskCard({ msg }: { msg: Message }) {
  return (
    <Card style={{ marginVertical: gap.xs }}>
      <Row style={{ justifyContent: 'space-between', marginBottom: gap.xs }}>
        <Row gap={6}>
          <View style={s.taskIcon}>
            <Icon name="document-text-outline" size={13} color={color.accent} />
          </View>
          <T size={type.caption} color={color.accent} weight="600">
            产出
          </T>
        </Row>
        <T size={type.caption} color={color.ink3}>
          {msg.agent}
        </T>
      </Row>
      <T size={type.headline} weight="600" numberOfLines={1}>
        {msg.artifacts?.[0]}
      </T>
      <T
size={type.sub}
        color={color.ink2}
        lh={leading.sub}
        numberOfLines={3}
        style={{ marginTop: 4 }}
      >
        {msg.content}
      </T>
      <Row style={{ marginTop: gap.sm }} gap={4}>
        <T size={type.caption} color={color.accent} weight="600">
          查看详情
        </T>
        <Icon name="chevron-forward" size={11} color={color.accent} />
      </Row>
    </Card>
  );
}

const s = StyleSheet.create({
  topbar: {
    height: 54,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: gap.sm,
    gap: gap.xxs,
  },
  topCenter: { flex: 1, alignItems: 'center' },
  membersWrap: {
    paddingBottom: gap.sm,
  },
  membersRow: {
    paddingHorizontal: PAGE_X,
    gap: gap.md,
  },
  memberItem: { alignItems: 'center', width: 56 },
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: gap.xs,
    marginHorizontal: PAGE_X,
    marginBottom: gap.xs,
    paddingVertical: 11,
    paddingHorizontal: gap.sm,
    backgroundColor: color.accentSoft,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: withAlpha(color.accent, 0.12),
  },
  bannerErr: { backgroundColor: color.badSoft },
  pulseDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: color.accent,
  },
  msgList: {
    paddingHorizontal: PAGE_X,
    paddingTop: gap.xs,
    paddingBottom: gap.lg,
  },
  userBubble: {
    backgroundColor: color.accent,
    borderRadius: radius.card,
    borderBottomRightRadius: 6,
    paddingHorizontal: gap.md,
    paddingVertical: 11,
    maxWidth: '84%',
  },
  agentRow: {
    flexDirection: 'row',
    gap: gap.xs,
    alignItems: 'flex-start',
  },
  agentBubble: {
    backgroundColor: color.card,
    borderRadius: radius.card,
    borderTopLeftRadius: 8,
    paddingHorizontal: gap.md,
    paddingVertical: 12,
    alignSelf: 'flex-start',
    ...shadow.card,
  },
  typingDot: {
    width: 5,
    height: 5,
    borderRadius: 3,
    backgroundColor: color.ink4,
  },
  taskIcon: {
    width: 20,
    height: 20,
    borderRadius: 7,
    backgroundColor: color.white,
    alignItems: 'center',
    justifyContent: 'center',
  },
  composer: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: gap.xs,
    paddingHorizontal: PAGE_X,
    paddingTop: gap.xs,
    paddingBottom: gap.xs,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: color.line,
    backgroundColor: color.bg,
  },
  input: {
    flex: 1,
    minHeight: 46,
    maxHeight: 130,
    backgroundColor: color.card,
    borderRadius: radius.card,
    paddingHorizontal: gap.md,
    paddingTop: 13,
    paddingBottom: 13,
    fontSize: type.body,
    color: color.ink,
    borderWidth: 1,
    borderColor: color.line,
  },
  sendBtn: {
    width: 44,
    height: 44,
    borderRadius: 22,
    backgroundColor: color.accent,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sendBtnOff: { backgroundColor: color.cardSunken },
  stopBtn: {
    width: 44,
    height: 44,
    borderRadius: 22,
    backgroundColor: color.ink,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stopSquare: { width: 12, height: 12, borderRadius: 3, backgroundColor: color.white },
});
