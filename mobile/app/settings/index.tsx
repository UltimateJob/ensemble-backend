import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../../src/components/Canvas';
import { Surface } from '../../src/components/Surface';
import { Button, Card, Icon, IconTile, Input, NavBar, Row, T, Tag, Toggle } from '../../src/components/ui';
import { del, get, getBaseURL, humanError, patch, setBaseURL } from '../../src/lib/api';
import { signOut } from '../../src/lib/session';
import { useApp } from '../../src/lib/store';
import type { Device } from '../../src/lib/types';
import { useTheme, useThemePref } from '../../src/theme/theme';
import { CARD_GAP, CARD_PAD, gap, leading, radius, type } from '../../src/theme/tokens';

/** 设置：外观 / 服务器 / 设备。也是「手机为什么能碰你电脑」的解释处。 */
export default function Settings() {
  const t = useTheme();
  const router = useRouter();
  const user = useApp((s) => s.user);
  const pref = useThemePref((s) => s.pref);
  const setPref = useThemePref((s) => s.setPref);
  const [devices, setDevices] = useState<Device[]>([]);
  const [err, setErr] = useState('');
  const [host, setHost] = useState(getBaseURL());

  const load = useCallback(async () => {
    try {
      const list = await get<Device[]>('/v1/devices');
      setDevices(Array.isArray(list) ? list : []);
    } catch (e) {
      setErr(humanError(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function toggleApproval(d: Device, v: boolean) {
    try {
      const u = await patch<Device>(`/v1/devices/${d.id}`, { requireApproval: v });
      setDevices((l) => l.map((x) => (x.id === d.id ? u : x)));
    } catch (e) {
      setErr(humanError(e));
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="设置" onBack={() => router.back()} />
        <ScrollView contentContainerStyle={s.page} keyboardShouldPersistTaps="handled">
          <Surface shell={4} outerRadius={22}><View style={{ padding: CARD_PAD }}>
            <Row gap={gap.sm}>
              <IconTile name="person" colors={t.gradient} size={46} iconSize={21} />
              <View style={{ flex: 1 }}>
                <T size={type.headline} weight="600">
                  {user?.name ?? '已连接'}
                </T>
                <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
                  额度 {user ? Math.max(0, (user.quota ?? 0) - (user.used ?? 0)) : '—'} / {user?.quota ?? '—'}
                </T>
              </View>
            </Row>
          </View></Surface>

          <Label>外观</Label>
          <Surface shell={4} outerRadius={22}><View style={{ padding: CARD_PAD }}>
            <Row gap={6}>
              {(['auto', 'light', 'dark'] as const).map((k) => (
                <Pressable
                  key={k}
                  onPress={() => setPref(k)}
                  style={[s.seg, pref === k && { backgroundColor: t.accentSoft }]}
                >
                  <T size={type.sub} color={pref === k ? t.accentText : t.ink2} weight={pref === k ? '700' : '400'}>
                    {k === 'auto' ? '跟随系统' : k === 'light' ? '浅色' : '深色'}
                  </T>
                </Pressable>
              ))}
            </Row>
          </View></Surface>

          <Label>服务器</Label>
          <Surface shell={4} outerRadius={22}><View style={{ padding: CARD_PAD }}>
            <View style={{ gap: gap.xs }}>
              <Input value={host} onChangeText={setHost} placeholder="192.168.1.5:8080" />
              <Button
                label="保存地址"
                variant="secondary"
                size="sm"
                onPress={async () => {
                  await setBaseURL(host).catch((e) => setErr(humanError(e)));
                  load();
                }}
              />
            </View>
          </View></Surface>

          <Label>我的设备</Label>
          <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ marginBottom: gap.sm }}>
            每台配对过的设备单独授权，吊销后立刻失效。
          </T>
          {err ? (
            <T size={type.sub} color={t.bad} style={{ marginBottom: gap.xs }}>
              {err}
            </T>
          ) : null}
          {devices.length === 0 ? (
            <Surface shell={4} outerRadius={22}><View style={{ padding: CARD_PAD }}>
              <T size={type.sub} color={t.ink3}>
                还没有配对的设备
              </T>
            </View></Surface>
          ) : (
            devices.map((d) => {
              const revoked = !!d.revokedAt;
              return (
                <Surface
                  key={d.id}
                  shell={4}
                  outerRadius={22}
                  style={{ marginBottom: CARD_GAP, opacity: revoked ? 0.6 : 1 }}
                >
                  <View style={{ padding: CARD_PAD }}>
                  <Row gap={gap.sm}>
                    <IconTile
                      name="phone-portrait"
                      colors={revoked ? ['#9AA0B4', '#6E7488'] : t.gradient}
                      size={40}
                      iconSize={18}
                    />
                    <View style={{ flex: 1 }}>
                      <Row gap={6}>
                        <T size={type.body} weight="600">
                          {d.name}
                        </T>
                        <Tag label={revoked ? '已吊销' : '有效'} tone={revoked ? 'blocked' : 'done'} />
                      </Row>
                      <T size={type.caption} color={t.ink3} style={{ marginTop: 2 }}>
                        {d.platform} · 最近活跃 {ago(d.lastSeen)}
                      </T>
                    </View>
                    {!revoked ? (
                      <Pressable
                        hitSlop={10}
                        onPress={() =>
                          Alert.alert('吊销设备', `「${d.name}」将失去访问权。`, [
                            { text: '取消', style: 'cancel' },
                            {
                              text: '吊销',
                              style: 'destructive',
                              onPress: async () => {
                                await del(`/v1/devices/${d.id}`).catch(() => {});
                                load();
                              },
                            },
                          ])
                        }
                      >
                        <T size={type.caption} color={t.bad} weight="600">
                          吊销
                        </T>
                      </Pressable>
                    ) : null}
                  </Row>
                  {!revoked ? (
                    <Row style={{ justifyContent: 'space-between', marginTop: gap.sm }}>
                      <View style={{ flex: 1, paddingRight: gap.sm }}>
                        <T size={type.sub}>执行前需要审批</T>
                        <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ marginTop: 2 }}>
                          关掉后按角色权限直接执行
                        </T>
                      </View>
                      <Toggle on={d.requireApproval} onChange={(v) => toggleApproval(d, v)} />
                    </Row>
                  ) : null}
                </View></Surface>
              );
            })
          )}

          <Label>配对新设备</Label>
          <Surface shell={4} outerRadius={22}><View style={{ padding: CARD_PAD }}>
            <Row gap={gap.xs} align="flex-start">
              <Icon name="key-outline" size={15} color={t.ink3} style={{ marginTop: 1 }} />
              <T size={type.sub} color={t.ink2} lh={leading.sub} style={{ flex: 1 }}>
                在电脑上打开纸团 → 设置 → 配对新设备，得到 6 位码。配对码 5 分钟内有效、只能用一次。
              </T>
            </Row>
          </View></Surface>

          <Button
            label="退出登录"
            variant="danger"
            full
            style={{ marginTop: gap.lg }}
            onPress={() =>
              Alert.alert('退出', '退出后需要重新配对或登录。', [
                { text: '取消', style: 'cancel' },
                {
                  text: '退出',
                  style: 'destructive',
                  onPress: async () => {
                    await signOut();
                    router.replace('/');
                  },
                },
              ])
            }
          />
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

function Label({ children }: { children: string }) {
  return (
    <T size={type.headline} weight="600" style={{ marginTop: gap.xl, marginBottom: gap.xs }}>
      {children}
    </T>
  );
}

function ago(iso: string) {
  if (!iso) return '—';
  const d = Date.now() - new Date(iso).getTime();
  if (d < 60000) return '刚刚';
  if (d < 3600000) return `${Math.floor(d / 60000)} 分钟前`;
  if (d < 86400000) return `${Math.floor(d / 3600000)} 小时前`;
  return `${Math.floor(d / 86400000)} 天前`;
}

const s = StyleSheet.create({
  page: { padding: gap.lg, paddingBottom: 100 },
  seg: { flex: 1, paddingVertical: 9, borderRadius: radius.control, alignItems: 'center' },
});
