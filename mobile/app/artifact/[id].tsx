import * as Clipboard from 'expo-clipboard';
import { useLocalSearchParams, useRouter } from 'expo-router';
import React, { useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Avatar } from '../../src/components/Avatar';
import { Canvas } from '../../src/components/Canvas';
import { Icon, NavBar, Row, T, Tag } from '../../src/components/ui';
import { get, humanError } from '../../src/lib/api';
import type { Artifact } from '../../src/lib/types';
import { useTheme } from '../../src/theme/theme';
import { gap, leading, radius, type } from '../../src/theme/tokens';

export default function ArtifactDetail() {
  const t = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const [a, setA] = useState<Artifact | null>(null);
  const [err, setErr] = useState('');
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        setA(await get<Artifact>(`/v1/library/${id}`));
      } catch (e) {
        setErr(humanError(e));
      }
    })();
  }, [id]);

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar
          title={a?.title ?? '产物'}
          onBack={() => router.back()}
          right={
            a ? (
              <Pressable
                hitSlop={10}
                onPress={async () => {
                  await Clipboard.setStringAsync(a.content ?? '');
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                }}
              >
                <Icon name={copied ? 'checkmark' : 'copy-outline'} size={20} color={copied ? t.ok : t.accent} />
              </Pressable>
            ) : null
          }
        />
        {!a && !err ? (
          <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}>
            <ActivityIndicator color={t.accent} />
          </View>
        ) : null}
        {err ? <T size={type.sub} color={t.bad} style={{ padding: gap.lg }}>{err}</T> : null}
        {a ? (
          <ScrollView contentContainerStyle={{ padding: gap.lg, gap: gap.md, paddingBottom: gap.xxxl }}>
            <Row gap={gap.xs}>
              <Avatar name={a.agentName ?? '?'} size={20} />
              <T size={type.sub} color={t.ink2}>{a.agentName ?? '系统'}</T>
              <Tag label={a.kind} tone="neutral" />
              {a.conversationTitle ? (
                <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ flex: 1 }}>{a.conversationTitle}</T>
              ) : null}
            </Row>
            <View style={{ backgroundColor: t.card, borderRadius: radius.card, padding: gap.md }}>
              <T size={type.sub} lh={leading.body} style={{ fontFamily: a.kind === 'code' ? 'Courier' : undefined }}>
                {a.content || '（空）'}
              </T>
            </View>
          </ScrollView>
        ) : null}
      </SafeAreaView>
    </Canvas>
  );
}
