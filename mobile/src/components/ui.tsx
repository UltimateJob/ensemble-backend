import { Ionicons } from '@expo/vector-icons';
import { BlurView } from 'expo-blur';
import { LinearGradient } from 'expo-linear-gradient';
import React from 'react';
import {
  ActivityIndicator,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
  type StyleProp,
  type TextStyle,
  type ViewStyle,
} from 'react-native';
import { Breathe, PressableScale } from './motion';
import { Glass } from './Glass';
import { useFontScaleValue, useTheme, type Theme } from '../theme/theme';
import { fontFamily } from '../theme/fonts';
import { gap, icon as iconSize, leading, radius, type } from '../theme/tokens';

export type IoniconName = React.ComponentProps<typeof Ionicons>['name'];
type GradientColors = readonly [string, string, ...string[]];
export const grad = (c: readonly string[]) => c as unknown as GradientColors;

export function withAlpha(hex: string, a: number): string {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return `rgba(${r},${g},${b},${a})`;
}

/** 统一的卡片阴影：亮色用蓝调淡影，暗色几乎没有影、改用描边。 */
export function cardShadow(t: Theme) {
  if (t.dark) {
    return {
      shadowColor: t.shadowColor,
      shadowOpacity: 0.5,
      shadowRadius: 16,
      shadowOffset: { width: 0, height: 6 },
      elevation: 2,
    };
  }
  return {
    shadowColor: t.shadowColor,
    shadowOpacity: 0.07,
    shadowRadius: 16,
    shadowOffset: { width: 0, height: 5 },
    elevation: 3,
  };
}

export function floatShadow(t: Theme) {
  return {
    shadowColor: t.shadowColor,
    shadowOpacity: t.dark ? 0.7 : 0.18,
    shadowRadius: t.dark ? 32 : 36,
    shadowOffset: { width: 0, height: 16 },
    elevation: 16,
  };
}

/**
 * 主色按钮的投影。
 *
 * 只留一点点：暗色 0.25、亮色 0.18，半径 12。
 * 之前的 0.45 + 半径 18 会在按钮周围形成一圈明显的发光——
 * 那是"AI 生成器"最典型的视觉指纹，真实产品不会这样。
 */
export function accentShadow(t: Theme) {
  return {
    shadowColor: t.dark ? '#3D5FD0' : '#4E7BFB',
    shadowOpacity: t.dark ? 0.22 : 0.16,
    shadowRadius: 10,
    shadowOffset: { width: 0, height: 4 },
    elevation: 4,
  };
}

/* ---------------- 文本 ---------------- */

export function T({
  children,
  size = type.body,
  color,
  weight = '400',
  lh,
  style,
  numberOfLines,
  align,
  onPress,
  tracking,
}: {
  children: React.ReactNode;
  size?: number;
  color?: string;
  weight?: TextStyle['fontWeight'] | string;
  lh?: number;
  style?: StyleProp<TextStyle>;
  numberOfLines?: number;
  align?: TextStyle['textAlign'];
  /** 少数场景把文字当链接用（跳过、关闭），省一层 Pressable */
  onPress?: () => void;
  /** 字距（px）。规范 §3：过线小字用 1.2 */
  tracking?: number;
}) {
  const t = useTheme();
  // 设置里的「字体大小」在这里生效：只缩放文字本身，行高同步缩放，
  // 间距和图标不动——否则小屏会把卡片撑破。
  const k = useFontScaleValue();
  const fs = Math.round(size * k * 10) / 10;
  return (
    <Text
      onPress={onPress}
      numberOfLines={numberOfLines}
      style={[
        {
          fontSize: fs,
          color: color ?? t.ink,
          fontWeight: weight as any,
          fontFamily: fontFamily(weight),
          lineHeight: Math.round((lh ?? size * 1.45) * k),
          textAlign: align,
          letterSpacing: tracking ?? (size >= type.title ? -0.45 : 0),
        },
        style,
      ]}
    >
      {children}
    </Text>
  );
}

/* ---------------- 图标 ---------------- */

export function Icon({
  name,
  size = iconSize.md,
  color,
  style,
}: {
  name: IoniconName;
  size?: number;
  color?: string;
  style?: StyleProp<TextStyle>;
}) {
  const t = useTheme();
  return <Ionicons name={name} size={size} color={color ?? t.ink2} style={style as any} />;
}

/** 渐变图标底：每个功能入口的色彩身份。 */
export function IconTile({
  name,
  size = 40,
  colors,
  iconSize: is = 20,
  radius: r = radius.sm,
  iconColor = '#FFFFFF',
}: {
  name: IoniconName;
  size?: number;
  colors: readonly string[];
  iconSize?: number;
  radius?: number;
  iconColor?: string;
}) {
  return (
    <LinearGradient
      colors={grad(colors)}
      start={{ x: 0, y: 0 }}
      end={{ x: 1, y: 1 }}
      style={{
        width: size,
        height: size,
        borderRadius: r,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Ionicons name={name} size={is} color={iconColor} />
    </LinearGradient>
  );
}

/** 柔和图标底（浅渐变 + 彩色图标），用于列表行。 */
export function IconTileSoft({
  name,
  size = 40,
  tint,
  iconSize: is = 19,
}: {
  name: IoniconName;
  size?: number;
  tint: string;
  iconSize?: number;
}) {
  return (
    <LinearGradient
      colors={grad([withAlpha(tint, 0.16), withAlpha(tint, 0.06)])}
      start={{ x: 0, y: 0 }}
      end={{ x: 1, y: 1 }}
      style={{
        width: size,
        height: size,
        borderRadius: radius.sm,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Ionicons name={name} size={is} color={tint} />
    </LinearGradient>
  );
}

/* ---------------- 按钮 ---------------- */

export function Button({
  label,
  onPress,
  variant = 'primary',
  size = 'md',
  disabled,
  loading,
  icon: leadingIcon,
  style,
  full,
}: {
  label: string;
  onPress?: () => void;
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'md' | 'sm';
  disabled?: boolean;
  loading?: boolean;
  icon?: IoniconName;
  style?: StyleProp<ViewStyle>;
  full?: boolean;
}) {
  const t = useTheme();
  const h = size === 'md' ? 52 : 40;
  const opacity = disabled ? 0.45 : 1;

  if (variant === 'primary') {
    return (
      <PressableScale
        onPress={onPress}
        disabled={disabled || loading}
        accessibilityRole="button"
        accessibilityLabel={label}
        scaleTo={0.97}
        style={[{ alignSelf: full ? 'stretch' : 'auto' }, style]}
      >
        <LinearGradient
          colors={grad(disabled ? (t.dark ? ['#2A2C38', '#2A2C38'] : ['#CBD5E8', '#CBCDE4']) : t.gradient)}
          start={{ x: 0, y: 0 }}
          end={{ x: 1, y: 1 }}
          style={[
            styles.btn,
            { height: h, paddingHorizontal: size === 'md' ? gap.xl : gap.md, opacity },
            !disabled && accentShadow(t),
          ]}
        >
          {loading ? (
            <ActivityIndicator size="small" color="#FFFFFF" />
          ) : (
            <View style={styles.btnRow}>
              {leadingIcon ? <Icon name={leadingIcon} size={16} color="#FFFFFF" /> : null}
              <Text style={{ color: '#FFFFFF', fontSize: size === 'md' ? type.body : type.sub, fontWeight: '700' }}>
                {label}
              </Text>
            </View>
          )}
        </LinearGradient>
      </PressableScale>
    );
  }

  const spec = {
    secondary: { bg: t.cardSunken, fg: t.ink },
    ghost: { bg: 'transparent', fg: t.accent },
    danger: { bg: t.badSoft, fg: t.bad },
  }[variant];

  return (
    <PressableScale
      onPress={onPress}
      disabled={disabled || loading}
      accessibilityRole="button"
      accessibilityLabel={label}
      style={[{ alignSelf: full ? 'stretch' : 'auto' }, style]}
    >
      <View
        style={[
          styles.btn,
          {
            height: h,
            backgroundColor: spec.bg,
            paddingHorizontal: size === 'md' ? gap.xl : gap.md,
            opacity,
          },
        ]}
      >
        {loading ? (
          <ActivityIndicator size="small" color={spec.fg} />
        ) : (
          <View style={styles.btnRow}>
            {leadingIcon ? <Icon name={leadingIcon} size={16} color={spec.fg} /> : null}
            <Text style={{ color: spec.fg, fontSize: size === 'md' ? type.body : type.sub, fontWeight: '600' }}>
              {label}
            </Text>
          </View>
        )}
      </View>
    </PressableScale>
  );
}

export function IconButton({
  name,
  onPress,
  size = 40,
  tint,
  bg,
  accessibilityLabel,
}: {
  name: IoniconName;
  onPress?: () => void;
  size?: number;
  tint?: string;
  bg?: string;
  accessibilityLabel?: string;
}) {
  const t = useTheme();
  return (
    <PressableScale onPress={onPress} accessibilityRole="button" accessibilityLabel={accessibilityLabel} scaleTo={0.9}>
      <View
        style={{
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: bg ?? t.cardSunken,
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Icon name={name} size={iconSize.md} color={tint ?? t.ink} />
      </View>
    </PressableScale>
  );
}

export function AccentIconButton({
  name,
  onPress,
  size = 44,
  accessibilityLabel,
}: {
  name: IoniconName;
  onPress?: () => void;
  size?: number;
  accessibilityLabel?: string;
}) {
  const t = useTheme();
  return (
    <PressableScale onPress={onPress} scaleTo={0.9} accessibilityLabel={accessibilityLabel}>
      <LinearGradient
        colors={grad(t.gradient)}
        start={{ x: 0, y: 0 }}
        end={{ x: 1, y: 1 }}
        style={[
          {
            width: size,
            height: size,
            borderRadius: size / 2,
            alignItems: 'center',
            justifyContent: 'center',
          },
          accentShadow(t),
        ]}
      >
        <Ionicons name={name} size={20} color="#FFFFFF" />
      </LinearGradient>
    </PressableScale>
  );
}

/* ---------------- 卡片 ---------------- */

export function Card({
  children,
  style,
  onPress,
  padded = true,
  tone = 'default',
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  onPress?: () => void;
  padded?: boolean;
  tone?: 'default' | 'tint';
}) {
  const t = useTheme();
  const base: StyleProp<ViewStyle>[] = [
    {
      backgroundColor: tone === 'tint' ? t.cardTint : t.card,
      borderRadius: radius.card,
      borderWidth: t.dark ? StyleSheet.hairlineWidth : 0,
      borderColor: t.dark ? t.cardBorder : 'transparent',
      ...cardShadow(t),
    },
    padded && { padding: gap.md },
    style,
  ];
  if (!onPress) return <View style={base}>{children}</View>;
  return (
    <PressableScale onPress={onPress} scaleTo={0.98} style={base}>
      {children}
    </PressableScale>
  );
}

/** 毛玻璃条：顶栏、浮层菜单、悬浮输入区。 */
export function GlassBar({
  children,
  style,
  intensity = 60,
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  intensity?: number;
}) {
  const t = useTheme();
  if (Platform.OS === 'web') {
    // Web 上 BlurView 效果不稳，直接用半透明底色
    return (
      <View style={[{ backgroundColor: t.barBg }, style]}>
        {children}
      </View>
    );
  }
  return (
    <BlurView intensity={intensity} tint={t.dark ? 'dark' : 'light'} style={style}>
      <View style={{ flex: 1, backgroundColor: t.dark ? 'rgba(16,17,24,0.55)' : 'rgba(255,255,255,0.55)' }}>
        {children}
      </View>
    </BlurView>
  );
}

/* ---------------- 输入 ---------------- */

export function Input({
  value,
  onChangeText,
  placeholder,
  secure,
  multiline,
  keyboardType,
  autoCapitalize,
  style,
  editable = true,
  autoFocus,
  secureTextEntry,
}: {
  value: string;
  onChangeText: (v: string) => void;
  placeholder?: string;
  secure?: boolean;
  multiline?: boolean;
  keyboardType?: 'default' | 'numeric' | 'phone-pad' | 'email-address' | 'url';
  autoCapitalize?: 'none' | 'sentences' | 'words' | 'characters';
  style?: StyleProp<ViewStyle>;
  editable?: boolean;
  autoFocus?: boolean;
  /** 兼容旧调用 */
  secureTextEntry?: boolean;
}) {
  const t = useTheme();
  return (
    <TextInput
      value={value}
      onChangeText={onChangeText}
      placeholder={placeholder}
      placeholderTextColor={t.ink4}
      secureTextEntry={secure ?? secureTextEntry}
      multiline={multiline}
      keyboardType={keyboardType}
      autoCapitalize={autoCapitalize ?? 'none'}
      editable={editable}
      autoFocus={autoFocus}
      style={[
        {
          minHeight: 50,
          borderRadius: radius.control,
          backgroundColor: t.cardSunken,
          paddingHorizontal: gap.md,
          fontSize: type.body,
          color: t.ink,
          // 一道极淡的边：没有它，浅色输入框在浅底上会"消失"
          borderWidth: 1,
          borderColor: t.dark ? 'rgba(255,255,255,0.06)' : 'rgba(16,20,45,0.05)',
        },
        multiline && { minHeight: 96, textAlignVertical: 'top', paddingTop: gap.sm },
        style,
      ]}
    />
  );
}

/* ---------------- 布局件 ---------------- */

export function Row({
  children,
  gap: g = gap.xs,
  style,
  align = 'center',
}: {
  children: React.ReactNode;
  gap?: number;
  style?: StyleProp<ViewStyle>;
  align?: ViewStyle['alignItems'];
}) {
  return <View style={[{ flexDirection: 'row', alignItems: align, gap: g }, style]}>{children}</View>;
}

export function Divider({ inset = 0 }: { inset?: number }) {
  const t = useTheme();
  return <View style={{ height: StyleSheet.hairlineWidth, backgroundColor: t.line, marginLeft: inset }} />;
}

export function Tag({
  label,
  tone = 'neutral',
  icon: ic,
}: {
  label: string;
  tone?: 'neutral' | 'working' | 'done' | 'blocked' | 'accent' | 'gold';
  icon?: IoniconName;
}) {
  const t = useTheme();
  const map = {
    neutral: { bg: t.cardSunken, fg: t.ink2 },
    working: { bg: t.accentSoft, fg: t.accentText },
    accent: { bg: t.accentSoft, fg: t.accentText },
    done: { bg: t.okSoft, fg: t.ok },
    blocked: { bg: t.badSoft, fg: t.bad },
    gold: { bg: t.goldBg, fg: t.goldFg },
  } as const;
  const c = map[tone];
  return (
    <View style={[styles.tag, { backgroundColor: c.bg }]}>
      {ic ? <Icon name={ic} size={11} color={c.fg} /> : null}
      <Text style={{ fontSize: type.caption, color: c.fg, fontWeight: '600' }}>{label}</Text>
    </View>
  );
}

export function Progress({ value, tone = 'accent' }: { value: number; tone?: 'accent' | 'ok' }) {
  const t = useTheme();
  const v = Math.max(0, Math.min(100, value));
  return (
    <View style={{ height: 6, borderRadius: 3, backgroundColor: t.cardSunken, overflow: 'hidden' }}>
      <LinearGradient
        colors={grad(tone === 'ok' ? [t.ok, withAlpha(t.ok, 0.65)] : [t.accent, t.accentDeep])}
        start={{ x: 0, y: 0 }}
        end={{ x: 1, y: 0 }}
        style={{ width: `${v}%`, height: 6, borderRadius: 3 }}
      />
    </View>
  );
}

export function Empty({
  icon: ic = 'sparkles-outline',
  illustration,
  title,
  hint,
  action,
}: {
  icon?: IoniconName;
  /** 定制插画。传了就替代图标——空状态配插画比配图标更像成品 */
  illustration?: React.ReactNode;
  title: string;
  hint?: string;
  action?: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <View style={styles.empty}>
      {illustration ? (
        <View>{illustration}</View>
      ) : (
        <LinearGradient colors={grad(t.gradientSoft)} start={{ x: 0, y: 0 }} end={{ x: 1, y: 1 }} style={styles.emptyIcon}>
          <Icon name={ic} size={20} color={t.accent} />
        </LinearGradient>
      )}
      <T size={type.headline} weight="600" align="center" style={{ marginTop: gap.md }}>
        {title}
      </T>
      {hint ? (
        <T size={type.sub} color={t.ink3} align="center" lh={leading.sub} style={{ marginTop: gap.xxs, maxWidth: 280 }}>
          {hint}
        </T>
      ) : null}
      {action ? <View style={{ marginTop: gap.lg }}>{action}</View> : null}
    </View>
  );
}

export function Toggle({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
  const t = useTheme();
  return (
    <Pressable onPress={() => onChange(!on)} accessibilityRole="switch" accessibilityState={{ checked: on }} hitSlop={8}>
      <View
        style={{
          width: 51,
          height: 31,
          borderRadius: 16,
          padding: 2.5,
          justifyContent: 'center',
          overflow: 'hidden',
          backgroundColor: on ? t.accent : t.cardSunken,
        }}
      >
        {on ? (
          <LinearGradient colors={grad(t.gradient)} start={{ x: 0, y: 0 }} end={{ x: 1, y: 1 }} style={StyleSheet.absoluteFill} />
        ) : null}
        <View style={[styles.knob, { alignSelf: on ? 'flex-end' : 'flex-start' }]} />
      </View>
    </Pressable>
  );
}

/** 顶部毛玻璃导航栏。 */
/**
 * 二级页导航栏。
 *
 * 和对话页一样用悬浮玻璃胶囊，全站只有一种顶栏形态——
 * 之前这页是贴边文字、那页是胶囊，看起来像两个人做的。
 */
export function NavBar({
  title,
  subtitle,
  onBack,
  right,
  left,
}: {
  title?: string;
  subtitle?: string;
  onBack?: () => void;
  right?: React.ReactNode;
  left?: React.ReactNode;
}) {
  const t = useTheme();
  return (
    <View style={styles.navFloat}>
      <Glass radius={26} style={styles.navGlass}>
        <View style={styles.navSlot}>
          {onBack ? <IconButton name="chevron-back" onPress={onBack} accessibilityLabel="返回" bg="transparent" /> : left}
        </View>
        <View style={styles.navCenter}>
          {title ? (
            <T size={type.body} weight="600" numberOfLines={1}>
              {title}
            </T>
          ) : null}
          {subtitle ? (
            <T size={type.caption} color={t.ink3} numberOfLines={1} style={{ marginTop: 1 }}>
              {subtitle}
            </T>
          ) : null}
        </View>
        <View style={[styles.navSlot, { alignItems: 'flex-end' }]}>{right}</View>
      </Glass>
    </View>
  );
}

export function SectionHead({
  title,
  action,
  style,
}: {
  title: string;
  action?: React.ReactNode;
  style?: StyleProp<ViewStyle>;
}) {
  return (
    <Row style={[{ justifyContent: 'space-between', marginBottom: gap.sm }, style]}>
      <T size={type.title} weight="600">
        {title}
      </T>
      {action}
    </Row>
  );
}

/** 呼吸点。 */
export function PulseDot({
  active = true,
  tone,
  size = 9,
}: {
  active?: boolean;
  tone?: string;
  size?: number;
}) {
  const t = useTheme();
  const c = tone ?? t.accent;
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {active ? (
        <Breathe active min={0.45} duration={1600}>
          <View
            style={{
              width: size,
              height: size,
              borderRadius: size / 2,
              backgroundColor: withAlpha(c, 0.3),
              position: 'absolute',
            }}
          />
        </Breathe>
      ) : null}
      <View style={{ width: size * 0.55, height: size * 0.55, borderRadius: size * 0.28, backgroundColor: c }} />
    </View>
  );
}

/** 文字按钮（规范 §6.7）：无底无框，灰字。 */
export function TextButton({
  label,
  onPress,
  tone = 'default',
  size = type.caption,
  disabled,
  fontWeight,
}: {
  label: string;
  onPress?: () => void;
  tone?: 'default' | 'danger';
  size?: number;
  disabled?: boolean;
  fontWeight?: string;
}) {
  const t = useTheme();
  const c = tone === 'danger' ? t.bad : t.slate;
  return (
    <Pressable onPress={onPress} disabled={disabled} hitSlop={8} style={{ opacity: disabled ? 0.4 : 1 }}>
      <Text style={{ fontSize: size, color: c, fontWeight: (fontWeight ?? '500') as any }}>{label}</Text>
    </Pressable>
  );
}

/** 单选圆点（规范 §6.8）：未选描边 2px，选中墨黑描边 + 内点。 */
export function RadioDot({ on, size = 20 }: { on: boolean; size?: number }) {
  const t = useTheme();
  return (
    <View
      style={{
        width: size,
        height: size,
        borderRadius: size / 2,
        borderWidth: 2,
        borderColor: on ? t.primaryBg : t.radioOff,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {on ? (
        <View
          style={{
            width: size * 0.48,
            height: size * 0.48,
            borderRadius: size * 0.24,
            backgroundColor: t.primaryBg,
          }}
        />
      ) : null}
    </View>
  );
}

/** 分段切换（规范 §6.10）：玻璃胶囊，选中墨黑底白字。 */
export function Segmented({
  items,
  index,
  onChange,
  style,
}: {
  items: string[];
  index: number;
  onChange: (i: number) => void;
  style?: StyleProp<ViewStyle>;
}) {
  const t = useTheme();
  return (
    <View
      style={[
        {
          flexDirection: 'row',
          backgroundColor: t.slab,
          borderRadius: radius.pill,
          padding: 3,
          borderWidth: StyleSheet.hairlineWidth,
          borderColor: t.hairline,
        },
        style,
      ]}
    >
      {items.map((label, i) => {
        const on = i === index;
        return (
          <Pressable
            key={label}
            onPress={() => onChange(i)}
            style={{
              paddingHorizontal: 20,
              paddingVertical: 6,
              borderRadius: radius.pill,
              backgroundColor: on ? t.primaryBg : 'transparent',
            }}
          >
            <Text
              style={{
                fontSize: 13.5,
                fontWeight: on ? '600' : '500',
                color: on ? t.primaryInk : t.ink3,
              }}
            >
              {label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  btn: { borderRadius: radius.control, alignItems: 'center', justifyContent: 'center', flexDirection: 'row' },
  btnRow: { flexDirection: 'row', alignItems: 'center', gap: 7 },
  tag: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 3,
    borderRadius: radius.pill,
    paddingHorizontal: 9,
    paddingVertical: 4,
  },
  empty: { alignItems: 'center', paddingVertical: gap.xxxl, paddingHorizontal: gap.lg },
  emptyIcon: { width: 64, height: 64, borderRadius: 22, alignItems: 'center', justifyContent: 'center' },
  knob: {
    width: 26,
    height: 26,
    borderRadius: 13,
    backgroundColor: '#FFFFFF',
    shadowColor: '#000',
    shadowOpacity: 0.16,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 2 },
    elevation: 3,
  },
  navFloat: { paddingHorizontal: gap.md, paddingTop: 6, paddingBottom: 4 },
  navGlass: { height: 48, flexDirection: 'row', alignItems: 'center', paddingHorizontal: 4 },
  navSlot: { width: 44, justifyContent: 'center' },
  navCenter: { flex: 1, alignItems: 'center' },
});
