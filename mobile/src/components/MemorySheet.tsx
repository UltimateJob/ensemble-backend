import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Icon, T } from './ui';
import { del, get, humanError } from '../lib/api';
import { useTheme } from '../theme/theme';
import { CARD_PAD, gap, leading, radius, type, weight } from '../theme/tokens';

/**
 * 记忆面板。
 *
 * AI 记住了什么，用户有权看见，也有权删掉——这不是可选的设置项，
 * 是这类产品的基本承诺。后端 /v1/memories 一直有，手机端一条没接。
 */

interface Memory {
  id: string;
  scope: string;
  kind: string;
  text: string;
  occurredAt: string;
  salience: number;
}

interface AgentMemories {
  characterId: string;
  characterName: string;
  relation?: Memory | null;
  memories: Memory[] | null;
}

interface Snapshot {
  conversation: Memory[] | null;
  agents: AgentMemories[] | null;
}

export function MemorySheet({
  visible,
  convId,
  onClose,
}: {
  visible: boolean;
  convId: string;
  onClose: () => void;
}) {
  const t = useTheme();
  const [data, setData] = useState<Snapshot | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!convId) return;
    try {
      const d = await get<Snapshot>(`/v1/memories?conversationId=${convId}`);
      setData(d);
      setErr('');
    } catch (e) {
      setErr(humanError(e));
    }
  }, [convId]);

  useEffect(() => {
    if (visible) {
      setData(null);
      void load();
    }
  }, [visible, load]);

  const conv = data?.conversation ?? [];
  const agents = data?.agents ?? [];
  const total = conv.length + agents.reduce((n, a) => n + (a.memories ?? []).length, 0);

  function clearAll() {
    Alert.alert('清空记忆', '这个群的群记忆会被删除，角色对你的个人记忆保留。', [
      { text: '取消', style: 'cancel' },
      {
        text: '清空',
        style: 'destructive',
        onPress: async () => {
          setBusy(true);
          try {
            await del(`/v1/memories?conversationId=${convId}`);
            await load();
          } catch (e) {
            setErr(humanError(e));
          } finally {
            setBusy(false);
          }
        },
      },
    ]);
  }

  return (
    <Modal visible={visible} animationType="slide" presentationStyle="pageSheet" onRequestClose={onClose}>
      <SafeAreaView style={{ flex: 1, backgroundColor: t.canvas[0] }} edges={['top', 'bottom']}>
        <View style={s.head}>
          <Pressable onPress={onClose} hitSlop={12} accessibilityLabel="关闭">
            <Icon name="close" size={24} color={t.ink3} />
          </Pressable>
          <T size={type.title} weight={weight.strong}>
            记忆
          </T>
          <Pressable
            onPress={clearAll}
            hitSlop={12}
            disabled={busy || total === 0}
            accessibilityRole="button"
            accessibilityLabel="清空群记忆"
          >
            <T size={type.caption} color={total === 0 ? t.ink4 : t.bad} weight={weight.medium}>
              清空
            </T>
          </Pressable>
        </View>

        <ScrollView contentContainerStyle={{ paddingHorizontal: gap.md, paddingBottom: 40 }}>
          <T size={type.caption} color={t.ink3} lh={leading.caption ?? 19} style={{ marginBottom: gap.md }}>
            这些是 AI 从你们的对话里记住的事。你随时可以删掉，删掉后它不会再拿这些回答你。
          </T>

          {err ? (
            <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
              {err}
            </T>
          ) : null}

          {!data ? (
            <View style={{ paddingTop: 50, alignItems: 'center' }}>
              <ActivityIndicator color={t.accent} />
            </View>
          ) : total === 0 ? (
            <View style={{ paddingTop: 60, alignItems: 'center', paddingHorizontal: gap.lg }}>
              <Icon name="sparkles-outline" size={30} color={t.ink4} />
              <T size={type.body} weight={weight.semibold} color={t.ink3} style={{ marginTop: 12 }}>
                还没有记住什么
              </T>
              <T size={type.caption} color={t.ink4} align="center" style={{ marginTop: 6 }}>
                多聊几次，它会把重要的事记下来
              </T>
            </View>
          ) : (
            <>
              {conv.length ? (
                <Section title="这个群">
                  {conv.map((m) => (
                    <MemoryRow key={m.id} m={m} />
                  ))}
                </Section>
              ) : null}

              {agents.map((a) =>
                (a.memories ?? []).length || a.relation ? (
                  <Section key={a.characterId} title={`${a.characterName} 记得`}>
                    {a.relation ? <MemoryRow m={a.relation} /> : null}
                    {(a.memories ?? []).map((m) => (
                      <MemoryRow key={m.id} m={m} />
                    ))}
                  </Section>
                ) : null,
              )}
            </>
          )}
        </ScrollView>
      </SafeAreaView>
    </Modal>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  const t = useTheme();
  return (
    <View style={{ marginBottom: gap.lg }}>
      <T size={type.caption} weight={weight.semibold} color={t.ink3} style={{ marginBottom: 8 }}>
        {title}
      </T>
      <View style={{ gap: 8 }}>{children}</View>
    </View>
  );
}

function MemoryRow({ m }: { m: Memory }) {
  const t = useTheme();
  return (
    <View style={[s.row, { backgroundColor: t.card, borderColor: t.line }]}>
      <View style={[s.dot, { backgroundColor: t.accentSoft }]}>
        <Icon name="ellipse" size={7} color={t.accent} />
      </View>
      <T size={type.sub} color={t.ink} lh={21} style={{ flex: 1 }}>
        {m.text}
      </T>
    </View>
  );
}

const s = StyleSheet.create({
  head: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: gap.md,
    paddingVertical: gap.sm,
  },
  row: {
    flexDirection: 'row',
    gap: 10,
    padding: CARD_PAD,
    borderRadius: radius.card,
    borderWidth: StyleSheet.hairlineWidth,
  },
  dot: {
    width: 18,
    height: 18,
    borderRadius: 9,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 3,
  },
});
