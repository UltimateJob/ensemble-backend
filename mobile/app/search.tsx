import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { Empty, Icon, NavBar, T } from '../src/components/ui';
import { get, humanError } from '../src/lib/api';
import { useTheme } from '../src/theme/theme';
import { CARD_PAD, PAGE_X, gap, radius, type, weight } from '../src/theme/tokens';

/**
 * 全局搜索。
 *
 * 后端 /v1/search 会同时搜群名和消息内容，一直没接。团队一多，
 * 用户想找「上次说的那个方案」就只能一个个群翻。
 */

interface Hit {
  convId: string;
  convTitle: string;
  msgId: string;
  agent?: string;
  role: string;
  content: string;
  createdAt: string;
  snippet: string;
}

export default function Search() {
  const t = useTheme();
  const router = useRouter();
  const [q, setQ] = useState('');
  const [hits, setHits] = useState<Hit[]>([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [searched, setSearched] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const run = useCallback(async (text: string) => {
    const key = text.trim();
    if (!key) {
      setHits([]);
      setSearched(false);
      return;
    }
    setLoading(true);
    try {
      const d = await get<{ hits: Hit[] }>(`/v1/search?q=${encodeURIComponent(key)}&limit=40`);
      setHits(d?.hits ?? []);
      setErr('');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setLoading(false);
      setSearched(true);
    }
  }, []);

  // 输入停顿 300ms 再发请求，边打字边搜不会把后端打满
  useEffect(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => void run(q), 300);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [q, run]);

  function open(h: Hit) {
    if (h.role === 'conv') {
      router.push(`/team/${h.convId}` as any);
    } else {
      router.push(`/team/${h.convId}?msg=${h.msgId}` as any);
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="搜索" onBack={() => router.back()} />

        <View style={[s.searchBox, { backgroundColor: t.card, borderColor: t.line }]}>
          <Icon name="search" size={17} color={t.ink4} />
          <TextInput
            value={q}
            onChangeText={setQ}
            placeholder="搜群名或聊天内容"
            placeholderTextColor={t.ink4}
            autoFocus
            autoCorrect={false}
            returnKeyType="search"
            style={[s.input, { color: t.ink }]}
          />
          {q ? (
            <Pressable onPress={() => setQ('')} hitSlop={10} accessibilityLabel="清空">
              <Icon name="close-circle" size={17} color={t.ink4} />
            </Pressable>
          ) : null}
        </View>

        <ScrollView
          contentContainerStyle={s.page}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
        >
          {err ? (
            <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
              {err}
            </T>
          ) : null}

          {loading && !hits.length ? (
            <View style={{ paddingTop: 60, alignItems: 'center' }}>
              <ActivityIndicator color={t.accent} />
            </View>
          ) : !q.trim() ? (
            <View style={{ paddingTop: 60 }}>
              <Empty icon="search-outline" title="搜点什么" hint="群名、说过的话都能搜到" />
            </View>
          ) : searched && !hits.length ? (
            <View style={{ paddingTop: 60 }}>
              <Empty
                icon="file-tray-outline"
                title="没有结果"
                hint={`没找到和「${q.trim()}」相关的内容`}
              />
            </View>
          ) : (
            <>
              <T size={type.caption} color={t.ink3} style={{ marginBottom: gap.sm, marginLeft: 2 }}>
                {hits.length} 条结果
              </T>
              <View style={{ gap: 8 }}>
                {hits.map((h) => (
                  <Pressable
                    key={`${h.convId}_${h.msgId || h.role}`}
                    onPress={() => open(h)}
                    accessibilityRole="button"
                    accessibilityLabel={`${h.convTitle} ${h.snippet}`}
                    style={({ pressed }) => [
                      s.hit,
                      { backgroundColor: t.card, borderColor: t.line },
                      pressed && { opacity: 0.7 },
                    ]}
                  >
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                      <Icon
                        name={h.role === 'conv' ? 'people-outline' : 'chatbubble-outline'}
                        size={13}
                        color={t.ink4}
                      />
                      <T
                        size={type.micro}
                        color={t.ink3}
                        weight={weight.semibold}
                        numberOfLines={1}
                        style={{ flex: 1 }}
                      >
                        {h.convTitle}
                        {h.agent ? ` · ${h.agent}` : ''}
                      </T>
                      <T size={10} color={t.ink4}>
                        {h.createdAt}
                      </T>
                    </View>
                    <T size={type.sub} color={t.ink} numberOfLines={2} style={{ marginTop: 7 }}>
                      {h.snippet || h.content}
                    </T>
                  </Pressable>
                ))}
              </View>
            </>
          )}
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

const s = StyleSheet.create({
  searchBox: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 9,
    marginHorizontal: PAGE_X,
    marginTop: gap.sm,
    height: 46,
    paddingHorizontal: 13,
    borderRadius: radius.sm,
    borderWidth: StyleSheet.hairlineWidth,
  },
  input: { flex: 1, fontSize: type.body, padding: 0 },
  page: { paddingHorizontal: PAGE_X, paddingTop: gap.md, paddingBottom: 40 },
  hit: {
    padding: CARD_PAD,
    borderRadius: radius.card,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
