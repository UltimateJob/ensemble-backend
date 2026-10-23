import { LinearGradient } from 'expo-linear-gradient';
import { useRouter } from 'expo-router';
import React, { useEffect, useState } from 'react';
import {
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp } from '../src/components/motion';
import { Button, Icon, Input, Row, T, grad } from '../src/components/ui';
import { humanError } from '../src/lib/api';
import { loginWithCode, pairWith, sendCode } from '../src/lib/session';
import { useApp } from '../src/lib/store';
import { useTheme } from '../src/theme/theme';
import { gap, leading, radius, type } from '../src/theme/tokens';

/**
 * 配对 / 登录。
 *
 * 第一屏要在一秒内回答三件事：这是什么、我要做什么、做了之后凭什么安全。
 */
export default function Entry() {
  const t = useTheme();
  const router = useRouter();
  const ready = useApp((s) => s.ready);
  const [tab, setTab] = useState<'pair' | 'login'>('pair');

  const [host, setHost] = useState('');
  const [code, setCode] = useState('');
  const [target, setTarget] = useState('');
  const [smsCode, setSmsCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [hint, setHint] = useState('');

  useEffect(() => {
    if (ready) router.replace('/(main)');
  }, [ready, router]);

  async function doPair() {
    setErr('');
    setHint('');
    if (!host.trim()) return setErr('先填电脑地址，比如 192.168.1.5:8080');
    if (!/^\d{6}$/.test(code.trim())) return setErr('配对码是 6 位数字');
    setBusy(true);
    try {
      await pairWith(host, code.trim(), Platform.OS === 'ios' ? 'iPhone' : 'Android', Platform.OS === 'ios' ? 'ios' : 'android');
      router.replace('/(main)');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  async function doSendCode() {
    setErr('');
    if (!target.trim()) return setErr('先填手机号或邮箱');
    if (!host.trim()) return setErr('先填电脑地址');
    setBusy(true);
    try {
      const r = await sendCode(host, target.trim());
      setHint(r?.devCode ? `验证码：${r.devCode}（本地演示直接回显）` : '验证码已发送');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  async function doLogin() {
    setErr('');
    if (!/^\d{6}$/.test(smsCode.trim())) return setErr('验证码是 6 位数字');
    setBusy(true);
    try {
      await loginWithCode(host, target.trim(), smsCode.trim());
      router.replace('/(main)');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
          <ScrollView contentContainerStyle={s.page} keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false}>
            <FadeInUp>
              <View style={{ alignItems: 'center', marginTop: gap.xxxl, marginBottom: gap.xxl }}>
                <LinearGradient
                  colors={grad(t.gradient)}
                  start={{ x: 0, y: 0 }}
                  end={{ x: 1, y: 1 }}
                  style={[s.mark, { shadowColor: t.dark ? '#1E2A5A' : '#2B3A77', shadowOpacity: t.dark ? 0.5 : 0.14, shadowRadius: 14, shadowOffset: { width: 0, height: 6 }, elevation: 6 }]}
                >
                  <Icon name="chatbubble-ellipses" size={30} color="#FFFFFF" />
                </LinearGradient>
                <T size={type.display} weight="600" style={{ marginTop: gap.lg }}>
                  纸团
                </T>
                <T size={type.sub} color={t.ink3} style={{ marginTop: 6 }}>
                  把一件事，揉成一群人
                </T>
              </View>
            </FadeInUp>

            <FadeInUp delay={60}>
              <View style={[s.seg, { backgroundColor: t.cardSunken }]}>
                {(['pair', 'login'] as const).map((k) => (
                  <Pressable
                    key={k}
                    onPress={() => {
                      setTab(k);
                      setErr('');
                      setHint('');
                    }}
                    style={[s.segItem, tab === k && { backgroundColor: t.card }]}
                  >
                    <T size={type.sub} weight={tab === k ? '700' : '400'} color={tab === k ? t.ink : t.ink3}>
                      {k === 'pair' ? '配对电脑' : '账号登录'}
                    </T>
                  </Pressable>
                ))}
              </View>
            </FadeInUp>

            <View style={{ gap: 18 }}>
              <Field label="电脑地址" icon="desktop-outline">
                <Input value={host} onChangeText={setHost} placeholder="192.168.1.5:8080" keyboardType="url" />
              </Field>

              {tab === 'pair' ? (
                <>
                  <Field label="配对码" icon="keypad-outline">
                    <Input value={code} onChangeText={setCode} placeholder="6 位数字" keyboardType="numeric" />
                  </Field>
                  {err ? <ErrorLine msg={err} /> : null}
                  <Button label="连接" onPress={doPair} loading={busy} full />
                </>
              ) : (
                <>
                  <Field label="手机号 / 邮箱" icon="person-outline">
                    <Input value={target} onChangeText={setTarget} placeholder="13800000000" />
                  </Field>
                  <Field label="验证码" icon="shield-checkmark-outline">
                    <Row gap={gap.xs}>
                      <View style={{ flex: 1 }}>
                        <Input value={smsCode} onChangeText={setSmsCode} placeholder="6 位数字" keyboardType="numeric" />
                      </View>
                      <Button label="获取" variant="secondary" onPress={doSendCode} disabled={busy} style={{ width: 78 }} />
                    </Row>
                  </Field>
                  {hint ? <T size={type.sub} color={t.ink3}>{hint}</T> : null}
                  {err ? <ErrorLine msg={err} /> : null}
                  <Button label="登录" onPress={doLogin} loading={busy} full />
                </>
              )}
            </View>

            <View style={[s.note, { backgroundColor: t.card, borderColor: t.cardBorder }]}>
              <Icon name="lock-closed-outline" size={15} color={t.ink3} />
              <T size={type.caption} color={t.ink3} lh={leading.caption} style={{ flex: 1 }}>
                配对后这台手机只能操作电脑上已绑定的工作区，不会获得整台电脑的访问权。随时可以在「设置 → 我的设备」里吊销。
              </T>
            </View>
          </ScrollView>
        </KeyboardAvoidingView>
      </SafeAreaView>
    </Canvas>
  );
}

function Field({ label, icon, children }: { label: string; icon: any; children: React.ReactNode }) {
  const t = useTheme();
  return (
    <View style={{ gap: 7 }}>
      <Row gap={5}>
        <Icon name={icon} size={13} color={t.ink3} />
        <T size={type.caption} color={t.ink2} weight="600">
          {label}
        </T>
      </Row>
      {children}
    </View>
  );
}

function ErrorLine({ msg }: { msg: string }) {
  const t = useTheme();
  return (
    <Row gap={5} align="flex-start">
      <Icon name="alert-circle-outline" size={13} color={t.bad} style={{ marginTop: 2 }} />
      <T size={type.sub} color={t.bad} lh={leading.sub} style={{ flex: 1 }}>
        {msg}
      </T>
    </Row>
  );
}

const s = StyleSheet.create({
  page: { paddingHorizontal: gap.lg, paddingBottom: gap.xxxl },
  mark: { width: 64, height: 64, borderRadius: 22, alignItems: 'center', justifyContent: 'center' },
  seg: { flexDirection: 'row', borderRadius: radius.control, padding: 3, marginBottom: gap.lg },
  segItem: { flex: 1, alignItems: 'center', paddingVertical: 9, borderRadius: radius.control - 3 },
  note: {
    flexDirection: 'row',
    gap: 7,
    alignItems: 'flex-start',
    marginTop: 32,
    padding: 14,
    borderRadius: radius.md,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
