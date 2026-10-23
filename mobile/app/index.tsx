import { Ionicons } from '@expo/vector-icons';
import { LinearGradient } from 'expo-linear-gradient';
import { useRouter } from 'expo-router';
import React, { useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  TextInput,
  useWindowDimensions,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Canvas } from '../src/components/Canvas';
import { FadeInUp, PressableScale } from '../src/components/motion';
import {
  Icon,
  Input,
  Row,
  T,
  TextButton,
  cardShadow,
  grad,
  type IoniconName,
} from '../src/components/ui';
import { humanError } from '../src/lib/api';
import {
  currentBase,
  loginWithCode,
  loginWithPassword,
  pairWith,
  sendCode,
} from '../src/lib/session';
import { useApp } from '../src/lib/store';
import { useTheme, type Theme } from '../src/theme/theme';
import { PAGE_X, gap, leading, radius, type } from '../src/theme/tokens';

/**
 * 入口 · 配对（附录 C 第 1 屏）。
 *
 * 首屏只做一件事：把手机连到电脑。所以自上而下是一条直线——
 * 品牌 → 6 位配对码 → 连接电脑 → 账号登录（次入口）。
 * 服务器地址是局域网场景的必需品，但它是配角：收在可折叠的小字里。
 */
export default function Entry() {
  const t = useTheme();
  const router = useRouter();
  const ready = useApp((s) => s.ready);

  const [host, setHost] = useState(currentBase());
  const [code, setCode] = useState('');
  const [target, setTarget] = useState('');
  const [smsCode, setSmsCode] = useState('');
  const [pwMode, setPwMode] = useState(false);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [hint, setHint] = useState('');

  const [loginOpen, setLoginOpen] = useState(false);
  const [serverOpen, setServerOpen] = useState(false);
  const [focused, setFocused] = useState(false);

  // 6 格 + 5 个 8px 间距要装进卡片内宽；窄屏上收成 44、保持 46 封顶。
  const { width: winW } = useWindowDimensions();
  const codeBoxW = Math.min(46, Math.floor((winW - PAGE_X * 2 - gap.xl * 2 - gap.xs * 5) / 6));

  const codeRef = useRef<TextInput>(null);
  /** 同一次 6 位数只自动提交一次；改一位后重新允许。 */
  const autoTried = useRef('');

  useEffect(() => {
    if (ready) router.replace('/(main)');
  }, [ready, router]);

  /* ---------------- 连接电脑 ---------------- */

  async function attemptPair(c: string) {
    if (busy) return;
    if (!host.trim()) {
      setServerOpen(true);
      setErr('先填电脑地址，再连接电脑');
      return;
    }
    setErr('');
    setHint('');
    setBusy(true);
    try {
      await pairWith(
        host.trim(),
        c,
        Platform.OS === 'ios' ? 'iPhone' : 'Android',
        Platform.OS === 'ios' ? 'ios' : 'android',
      );
      router.replace('/(main)');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  function doPair() {
    if (!/^\d{6}$/.test(code)) {
      setErr('配对码是 6 位数字');
      return;
    }
    void attemptPair(code);
  }

  // 输满自动提交：配对码没有「下一步」按钮，填完就该发生事情。
  useEffect(() => {
    if (code.length !== 6) {
      autoTried.current = '';
      return;
    }
    if (autoTried.current === code) return;
    autoTried.current = code;
    void attemptPair(code);
    // attemptPair 读取的是当前渲染的 host / busy
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [code]);

  /* ---------------- 账号登录 ---------------- */

  async function doSendCode() {
    setErr('');
    setHint('');
    if (!target.trim()) return setErr('先填手机号或邮箱');
    if (!host.trim()) {
      setServerOpen(true);
      return setErr('先填电脑地址');
    }
    setBusy(true);
    try {
      const r = await sendCode(host.trim(), target.trim());
      setHint(r?.devCode ? `验证码：${r.devCode}（本地演示直接回显）` : '验证码已发送');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  async function doLogin() {
    setErr('');
    if (!target.trim()) return setErr('先填手机号或邮箱');
    if (!host.trim()) {
      setServerOpen(true);
      return setErr('先填电脑地址');
    }
    if (pwMode) {
      if (!password) return setErr('先填密码');
    } else if (!/^\d{6}$/.test(smsCode.trim())) {
      return setErr('验证码是 6 位数字');
    }
    setBusy(true);
    try {
      if (pwMode) await loginWithPassword(host.trim(), target.trim(), password);
      else await loginWithCode(host.trim(), target.trim(), smsCode.trim());
      router.replace('/(main)');
    } catch (e) {
      setErr(humanError(e));
    } finally {
      setBusy(false);
    }
  }

  const boxBorder = t.track;

  return (
    <Canvas>
      <SafeAreaView style={{ flex: 1 }} edges={['top', 'bottom']}>
        <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
          <ScrollView
            contentContainerStyle={s.page}
            keyboardShouldPersistTaps="handled"
            showsVerticalScrollIndicator={false}
          >
            {/* 品牌 */}
            <FadeInUp>
              <View style={s.brand}>
                <LinearGradient
                  colors={grad(t.gradient)}
                  start={{ x: 0, y: 0 }}
                  end={{ x: 1, y: 1 }}
                  style={[s.mark, brandShadow(t)]}
                >
                  <T size={30} weight="750" color={t.white} lh={36}>
                    团
                  </T>
                </LinearGradient>
                <T size={type.display} weight="750" style={{ marginTop: gap.lg }}>
                  纸团
                </T>
                <T size={type.sub} color={t.ink3} style={{ marginTop: 6 }}>
                  把一件事，揉成一群人
                </T>
              </View>
            </FadeInUp>

            {/* 主卡：配对 */}
            <FadeInUp delay={60}>
              <View style={[s.card, { backgroundColor: t.card }, cardShadow(t)]}>
                <T size={17.5} weight="650">
                  连接你的电脑
                </T>
                <T size={14} color={t.ink3} lh={21} style={{ marginTop: 7 }}>
                  在电脑端打开纸团，把屏幕上的 6 位数字填进来，团队就在这台电脑上跑。
                </T>

                {/* 6 位配对码：一个透明输入层盖在方格上，体验最顺 */}
                <Pressable
                  onPress={() => codeRef.current?.focus()}
                  accessibilityRole="button"
                  accessibilityLabel="配对码，6 位数字"
                  style={s.codeWrap}
                >
                  <Row gap={gap.xs} style={{ flex: 1 }}>
                    {Array.from({ length: 6 }).map((_, i) => {
                      const digit = code[i] ?? '';
                      const active = focused && i === code.length && code.length < 6;
                      return (
                        <View
                          key={i}
                          style={[
                            s.codeBox,
                            {
                              width: codeBoxW,
                              backgroundColor: t.card,
                              borderColor: active ? t.accent : boxBorder,
                              borderWidth: active ? 2 : 1,
                            },
                          ]}
                        >
                          {digit ? (
                            <T size={24} weight="600" lh={30}>
                              {digit}
                            </T>
                          ) : active ? (
                            <Caret color={t.accent} />
                          ) : null}
                        </View>
                      );
                    })}
                  </Row>
                  <TextInput
                    ref={codeRef}
                    value={code}
                    onChangeText={(v) => {
                      setCode(v.replace(/\D/g, '').slice(0, 6));
                      setErr('');
                    }}
                    onFocus={() => setFocused(true)}
                    onBlur={() => setFocused(false)}
                    keyboardType="number-pad"
                    maxLength={6}
                    caretHidden
                    autoComplete="one-time-code"
                    textContentType="oneTimeCode"
                    style={s.codeInput}
                    accessibilityLabel="配对码输入"
                  />
                </Pressable>

                {err ? <ErrorLine msg={err} /> : null}

                <BrandButton label="连接电脑" onPress={doPair} loading={busy} />
              </View>
            </FadeInUp>

            {/* 或 */}
            <FadeInUp delay={110}>
              <Row gap={gap.md} style={{ marginVertical: gap.xl }}>
                <View style={[s.rule, { backgroundColor: t.line }]} />
                <T size={type.caption} color={t.ink3}>
                  或
                </T>
                <View style={[s.rule, { backgroundColor: t.line }]} />
              </Row>
            </FadeInUp>

            {/* 账号登录（次入口） */}
            <FadeInUp delay={150}>
              <PressableScale
                onPress={() => {
                  setLoginOpen((v) => !v);
                  setErr('');
                  setHint('');
                }}
                scaleTo={0.98}
                accessibilityRole="button"
              >
                <View
                  style={[
                    s.secondary,
                    {
                      backgroundColor: t.card,
                      borderColor: t.dark ? t.cardBorder : t.line,
                    },
                    cardShadow(t),
                  ]}
                >
                  <T size={15.5} weight="600">
                    使用账号登录
                  </T>
                </View>
              </PressableScale>
            </FadeInUp>

            {loginOpen ? (
              <FadeInUp>
                <View style={[s.loginCard, { backgroundColor: t.card, borderColor: t.cardBorder }, cardShadow(t)]}>
                  <Field label="手机号 / 邮箱" icon="person-outline">
                    <Input
                      value={target}
                      onChangeText={setTarget}
                      placeholder="13800000000"
                      keyboardType="email-address"
                    />
                  </Field>

                  {pwMode ? (
                    <Field label="密码" icon="lock-closed-outline" style={{ marginTop: gap.md }}>
                      <Input
                        value={password}
                        onChangeText={setPassword}
                        placeholder="至少 8 位"
                        secure
                      />
                    </Field>
                  ) : (
                    <Field label="验证码" icon="shield-checkmark-outline" style={{ marginTop: gap.md }}>
                      <Row gap={gap.xs}>
                        <View style={{ flex: 1 }}>
                          <Input
                            value={smsCode}
                            onChangeText={setSmsCode}
                            placeholder="6 位数字"
                            keyboardType="numeric"
                          />
                        </View>
                        <TextButton
                          label="获取验证码"
                          onPress={doSendCode}
                          disabled={busy}
                          size={type.caption}
                        />
                      </Row>
                    </Field>
                  )}

                  {hint ? (
                    <T size={type.caption} color={t.ink3} style={{ marginTop: gap.xs }}>
                      {hint}
                    </T>
                  ) : null}

                  <View style={{ marginTop: gap.sm }}>
                    <TextButton
                      label={pwMode ? '改用验证码登录' : '用密码登录'}
                      onPress={() => {
                        setPwMode((v) => !v);
                        setErr('');
                      }}
                      size={type.caption}
                    />
                  </View>

                  {err ? <ErrorLine msg={err} /> : null}

                  <BrandButton label="登录" onPress={doLogin} loading={busy} />
                </View>
              </FadeInUp>
            ) : null}

            {/* 服务器地址：局域网必需品，收成一行小字 */}
            <View style={{ marginTop: gap.lg, alignItems: 'center' }}>
              <Pressable
                onPress={() => setServerOpen((v) => !v)}
                hitSlop={8}
                accessibilityRole="button"
                style={s.serverToggle}
              >
                <Icon name="desktop-outline" size={13} color={t.ink4} />
                <T size={type.caption} color={t.ink4}>
                  {host ? `电脑地址 ${prettyHost(host)}` : '设置电脑地址'}
                </T>
                <Icon
                  name={serverOpen ? 'chevron-up' : 'chevron-down'}
                  size={12}
                  color={t.ink4}
                />
              </Pressable>
              {serverOpen ? (
                <FadeInUp distance={6}>
                  <View style={{ marginTop: gap.sm, width: '100%' }}>
                    <Input
                      value={host}
                      onChangeText={setHost}
                      placeholder="192.168.1.5:8080"
                      keyboardType="url"
                    />
                    <T size={type.micro} color={t.ink4} lh={16} style={{ marginTop: 6, textAlign: 'center' }}>
                      同一局域网：电脑端启动纸团后，填它显示的地址
                    </T>
                  </View>
                </FadeInUp>
              ) : null}
            </View>

            {/* 三个特性 */}
            <FadeInUp delay={200}>
              <Row gap={gap.sm} style={{ marginTop: gap.xxxl }}>
                <Feature icon="desktop-outline" label="跑在你的电脑上" />
                <Feature icon="people-outline" label="多 Agent 组队干活" />
                <Feature icon="shield-checkmark-outline" label="每一步你说了算" />
              </Row>
            </FadeInUp>

            {/* 底部说明 */}
            <View style={{ marginTop: gap.xxl, alignItems: 'center', gap: 2 }}>
              <T size={type.micro} color={t.ink4} align="center" lh={17}>
                同一局域网直连 · 数据保存在你自己的电脑上
              </T>
              <T size={type.micro} color={t.ink4} align="center" lh={17}>
                不经过任何第三方服务器
              </T>
            </View>
          </ScrollView>
        </KeyboardAvoidingView>
      </SafeAreaView>
    </Canvas>
  );
}

/* ---------------- 局部组件 ---------------- */

function brandShadow(t: Theme) {
  return {
    shadowColor: t.brandShadow,
    shadowOpacity: 1,
    shadowRadius: 24,
    shadowOffset: { width: 0, height: 12 },
    elevation: 8,
  };
}

/** 品牌渐变主按钮：全宽胶囊 + 品牌投影（附录 A1）。 */
function BrandButton({
  label,
  onPress,
  loading,
}: {
  label: string;
  onPress?: () => void;
  loading?: boolean;
}) {
  const t = useTheme();
  return (
    <PressableScale
      onPress={onPress}
      disabled={loading}
      scaleTo={0.97}
      accessibilityRole="button"
      accessibilityLabel={label}
      style={{ marginTop: gap.lg }}
    >
      <LinearGradient
        colors={grad(t.gradient)}
        start={{ x: 0, y: 0 }}
        end={{ x: 1, y: 1 }}
        style={[s.primary, brandShadow(t)]}
      >
        {loading ? (
          <ActivityIndicator size="small" color={t.white} />
        ) : (
          <T size={15.5} weight="650" color={t.white}>
            {label}
          </T>
        )}
      </LinearGradient>
    </PressableScale>
  );
}

/** 当前输入位的光标。没聚焦时不渲染。 */
function Caret({ color }: { color: string }) {
  const v = useRef(new Animated.Value(1)).current;
  useEffect(() => {
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(v, { toValue: 0, duration: 520, useNativeDriver: true }),
        Animated.timing(v, { toValue: 1, duration: 520, useNativeDriver: true }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [v]);
  return (
    <Animated.View
      style={{ width: 2, height: 22, borderRadius: 1, backgroundColor: color, opacity: v }}
    />
  );
}

function Field({
  label,
  icon,
  children,
  style,
}: {
  label: string;
  icon: IoniconName;
  children: React.ReactNode;
  style?: object;
}) {
  const t = useTheme();
  return (
    <View style={[{ gap: 7 }, style]}>
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
    <Row gap={5} align="flex-start" style={{ marginTop: gap.sm }}>
      <Icon name="alert-circle-outline" size={13} color={t.bad} style={{ marginTop: 2 }} />
      <T size={type.caption} color={t.bad} lh={leading.caption} style={{ flex: 1 }}>
        {msg}
      </T>
    </Row>
  );
}

function Feature({ icon, label }: { icon: IoniconName; label: string }) {
  const t = useTheme();
  return (
    <View style={s.feature}>
      <View style={[s.featureIcon, { backgroundColor: t.accentSoft }]}>
        <Ionicons name={icon} size={20} color={t.accent} />
      </View>
      <T size={type.caption} color={t.ink2} align="center" weight="550" lh={17}>
        {label}
      </T>
    </View>
  );
}

/** `http://192.168.1.5:8080` → `192.168.1.5:8080`，小字里别放协议。 */
function prettyHost(h: string): string {
  return h.replace(/^https?:\/\//i, '').replace(/\/+$/, '');
}

const s = StyleSheet.create({
  page: { paddingHorizontal: PAGE_X, paddingBottom: gap.xxl },
  brand: { alignItems: 'center', marginTop: gap.xxl },
  mark: { width: 64, height: 64, borderRadius: 20, alignItems: 'center', justifyContent: 'center' },
  card: { marginTop: gap.xxl, borderRadius: radius.card, padding: gap.xl },
  codeWrap: { marginTop: gap.lg, flexDirection: 'row' },
  codeBox: {
    height: 56,
    borderRadius: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  codeInput: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    opacity: 0,
    fontSize: 1,
    color: 'transparent',
  },
  primary: {
    height: 54,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
  },
  secondary: {
    height: 52,
    borderRadius: radius.pill,
    borderWidth: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  rule: { flex: 1, height: StyleSheet.hairlineWidth },
  loginCard: {
    marginTop: gap.md,
    padding: gap.lg,
    borderRadius: radius.card,
    borderWidth: StyleSheet.hairlineWidth,
  },
  serverToggle: { flexDirection: 'row', alignItems: 'center', gap: 5, paddingVertical: 4 },
  feature: { flex: 1, alignItems: 'center', gap: gap.xs },
  featureIcon: {
    width: 46,
    height: 46,
    borderRadius: 23,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
