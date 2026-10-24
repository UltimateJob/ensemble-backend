import { useRouter } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { Card, Divider, Icon, NavBar, Progress, Row, T, cardShadow } from '../src/components/ui';
import { get, humanError, post } from '../src/lib/api';
import { useApp } from '../src/lib/store';
import { useTheme } from '../src/theme/theme';
import { CARD_GAP, CARD_PAD, PAGE_X, gap, leading, radius, type, weight } from '../src/theme/tokens';

/**
 * 用量与额度。
 *
 * 之前手机端只有一个「额度 100%」的进度条，用户看不到总量、已用、
 * 什么时候重置，也找不到充值入口——后端明明有完整的钱包与兑换码，
 * 手机端一条都没接。
 */

interface Txn {
  id: string;
  kind: string;
  amountCny: number;
  note?: string;
  createdAt: string;
}

interface Wallet {
  balanceCny: number;
  totalTopup: number;
  totalSpent: number;
  currency: string;
  txns: Txn[] | null;
}

export default function Usage() {
  const t = useTheme();
  const router = useRouter();
  const user = useApp((s) => s.user);

  const [w, setW] = useState<Wallet | null>(null);
  const [err, setErr] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  const [code, setCode] = useState('');
  const [redeeming, setRedeeming] = useState(false);
  const [redeemMsg, setRedeemMsg] = useState('');

  const load = useCallback(async () => {
    try {
      const d = await get<Wallet>('/v1/wallet');
      setW(d);
      setErr('');
    } catch (e) {
      setErr(humanError(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function redeem() {
    const c = code.trim();
    if (!c) return;
    setRedeeming(true);
    setRedeemMsg('');
    try {
      await post('/v1/redeem', { code: c });
      setCode('');
      setRedeemMsg('兑换成功');
      await load();
    } catch (e) {
      setRedeemMsg(humanError(e));
    } finally {
      setRedeeming(false);
    }
  }

  const quota = Math.max(1, user?.quota ?? 0);
  const used = Math.max(0, user?.used ?? 0);
  const left = Math.max(0, quota - used);
  // 用掉 2/800 时四舍五入成 0%，和下面「已用 2 次」自相矛盾。
  // 有消耗但不足 1% 时向上取到 1%，宁可略高也不要显示成 0。
  const raw = (used / quota) * 100;
  const pct = raw > 0 && raw < 1 ? 1 : Math.round(raw);

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <NavBar title="用量与额度" onBack={() => router.back()} />
        <ScrollView
          contentContainerStyle={s.page}
          showsVerticalScrollIndicator={false}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              tintColor={t.accent}
              onRefresh={async () => {
                setRefreshing(true);
                await load();
                setRefreshing(false);
              }}
            />
          }
        >
          {err ? (
            <T size={type.caption} color={t.bad} style={{ marginBottom: gap.sm }}>
              {err}
            </T>
          ) : null}

          {/* 本周期额度 */}
          <FadeInUp>
            <Card style={{ padding: CARD_PAD }}>
              <Row style={{ justifyContent: 'space-between' }}>
                <T size={type.caption} color={t.ink3} weight={weight.semibold}>
                  本周期额度
                </T>
                <T size={type.caption} color={t.ink4}>
                  {pct >= 100 ? '已用完' : `已用 ${pct}%`}
                </T>
              </Row>

              <Row gap={7} align="flex-end" style={{ marginTop: 8 }}>
                <T size={34} weight={weight.bold} lh={38}>
                  {left}
                </T>
                <T size={type.sub} color={t.ink3} style={{ marginBottom: 5 }}>
                  次可用
                </T>
              </Row>

              <View style={{ marginTop: 12 }}>
                <Progress value={pct} />
              </View>

              <T size={type.micro} color={t.ink4} style={{ marginTop: 9 }}>
                共 {quota} 次 · 已用 {used} 次 · {resetLabel()} 重置
              </T>
            </Card>
          </FadeInUp>

          {/* 余额 */}
          <FadeInUp delay={40}>
            <Card style={{ marginTop: CARD_GAP, padding: CARD_PAD }}>
              <Row style={{ justifyContent: 'space-between' }}>
                <T size={type.caption} color={t.ink3} weight={weight.semibold}>
                  账户余额
                </T>
                <T size={type.caption} color={t.ink4}>
                  {w ? `累计充值 ¥${w.totalTopup.toFixed(2)}` : ''}
                </T>
              </Row>
              <Row gap={6} align="flex-end" style={{ marginTop: 8 }}>
                <T size={30} weight={weight.bold} lh={34}>
                  ¥{w ? w.balanceCny.toFixed(2) : '—'}
                </T>
              </Row>
              <T size={type.micro} color={t.ink4} style={{ marginTop: 8 }}>
                {w && w.totalSpent > 0
                  ? `累计消耗 ¥${w.totalSpent.toFixed(2)}`
                  : '按实际用量扣费，额度用完时从这里扣'}
              </T>
            </Card>
          </FadeInUp>

          {/* 兑换码 */}
          <FadeInUp delay={70}>
            <T
              size={type.caption}
              color={t.ink3}
              weight={weight.medium}
              style={{ marginTop: gap.xl, marginBottom: gap.sm, marginLeft: 2 }}
            >
              兑换码
            </T>
            <Card style={{ padding: CARD_PAD }}>
              <Row gap={gap.sm}>
                <View style={[s.codeBox, { borderColor: t.line, backgroundColor: t.cardSunken }]}>
                  <TextInput
                    value={code}
                    onChangeText={setCode}
                    placeholder="输入兑换码"
                    placeholderTextColor={t.ink4}
                    autoCapitalize="characters"
                    autoCorrect={false}
                    style={[s.codeInput, { color: t.ink }]}
                  />
                </View>
                <Pressable
                  onPress={redeem}
                  disabled={!code.trim() || redeeming}
                  accessibilityRole="button"
                  accessibilityLabel="兑换"
                  style={[
                    s.redeemBtn,
                    {
                      backgroundColor: code.trim() && !redeeming ? t.ink : t.toggleOff,
                    },
                  ]}
                >
                  {redeeming ? (
                    <ActivityIndicator size="small" color={t.primaryInk} />
                  ) : (
                    <T
                      size={type.sub}
                      weight={weight.semibold}
                      color={code.trim() ? t.primaryInk : t.ink3}
                    >
                      兑换
                    </T>
                  )}
                </Pressable>
              </Row>
              {redeemMsg ? (
                <T
                  size={type.caption}
                  color={redeemMsg === '兑换成功' ? t.ok : t.bad}
                  style={{ marginTop: 9 }}
                >
                  {redeemMsg}
                </T>
              ) : null}
            </Card>
          </FadeInUp>

          {/* 流水 */}
          <FadeInUp delay={100}>
            <T
              size={type.caption}
              color={t.ink3}
              weight={weight.medium}
              style={{ marginTop: gap.xl, marginBottom: gap.sm, marginLeft: 2 }}
            >
              最近流水
            </T>
            {!w ? (
              <View style={{ paddingVertical: gap.xl, alignItems: 'center' }}>
                <ActivityIndicator color={t.accent} />
              </View>
            ) : (w.txns ?? []).length === 0 ? (
              <Card style={{ padding: CARD_PAD, alignItems: 'center' }}>
                <T size={type.sub} color={t.ink3}>
                  还没有记录
                </T>
                <T size={type.micro} color={t.ink4} style={{ marginTop: 5 }} align="center">
                  充值和消耗都会出现在这里
                </T>
              </Card>
            ) : (
              <Card padded={false} style={cardShadow(t)}>
                {(w.txns ?? []).slice(0, 20).map((x, i, arr) => (
                  <View key={x.id}>
                    {i > 0 ? <Divider inset={CARD_PAD} /> : null}
                    <Row style={{ paddingHorizontal: CARD_PAD, paddingVertical: 13 }} gap={12}>
                      <View
                        style={[
                          s.txnIcon,
                          { backgroundColor: x.amountCny >= 0 ? t.okSoft : t.cardSunken },
                        ]}
                      >
                        <Icon
                          name={x.amountCny >= 0 ? 'add' : 'remove'}
                          size={15}
                          color={x.amountCny >= 0 ? t.ok : t.ink3}
                        />
                      </View>
                      <View style={{ flex: 1, minWidth: 0 }}>
                        <T size={type.sub} weight={weight.medium} numberOfLines={1}>
                          {x.note || (x.amountCny >= 0 ? '充值' : '消耗')}
                        </T>
                        <T size={type.micro} color={t.ink4} style={{ marginTop: 2 }}>
                          {fmtTime(x.createdAt)}
                        </T>
                      </View>
                      <T
                        size={type.sub}
                        weight={weight.semibold}
                        color={x.amountCny >= 0 ? t.ok : t.ink}
                      >
                        {x.amountCny >= 0 ? '+' : ''}
                        {x.amountCny.toFixed(2)}
                      </T>
                    </Row>
                  </View>
                ))}
              </Card>
            )}
          </FadeInUp>

          <Pressable
            onPress={() =>
              Alert.alert(
                '怎么获得额度',
                '联系团队获取兑换码，在「我 → 用量与额度」里兑换即可。额度用完前会在这里显示剩余次数。',
              )
            }
            style={{ marginTop: gap.xl, alignItems: 'center' }}
            accessibilityRole="button"
          >
            <T size={type.caption} color={t.ink4}>
              怎么获得额度？
            </T>
          </Pressable>
        </ScrollView>
      </SafeAreaView>
    </Canvas>
  );
}

/** 额度按自然月重置，和下月 1 日对齐。 */
function resetLabel() {
  const now = new Date();
  const next = new Date(now.getFullYear(), now.getMonth() + 1, 1);
  return `${next.getMonth() + 1} 月 1 日`;
}

function fmtTime(iso: string) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return '—';
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(
    d.getMinutes(),
  ).padStart(2, '0')}`;
}

const s = StyleSheet.create({
  page: { paddingHorizontal: PAGE_X, paddingTop: gap.sm, paddingBottom: 40 },
  codeBox: {
    flex: 1,
    height: 48,
    borderRadius: radius.sm,
    borderWidth: StyleSheet.hairlineWidth,
    justifyContent: 'center',
    paddingHorizontal: 12,
  },
  codeInput: { fontSize: type.body, padding: 0 },
  redeemBtn: {
    height: 48,
    paddingHorizontal: 22,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  txnIcon: {
    width: 30,
    height: 30,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
