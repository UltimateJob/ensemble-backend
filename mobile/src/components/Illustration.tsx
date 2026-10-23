import React from 'react';
import Svg, { Circle, Defs, G, LinearGradient, Path, Rect, Stop } from 'react-native-svg';
import { useTheme } from '../theme/theme';

/**
 * 空状态插画。
 *
 * 用 SVG 手绘，不引外部图片——好处是能跟随主题变色，
 * 而且在任何分辨率下都清晰。风格统一为「细线条 + 柔和的实心块」，
 * 和界面的线性图标同源。
 *
 * 为什么值得画：一个只有图标 + 文字的空白页，看起来像"没做完"；
 * 一张克制的插画能让空状态变成"这里将有东西"的邀请。
 */

/** 资料库空态：一叠纸 + 一颗星。 */
export function EmptyLibrary({ size = 120 }: { size?: number }) {
  const t = useTheme();
  const c1 = t.dark ? 'rgba(124,155,255,0.16)' : 'rgba(61,107,245,0.10)';
  const c2 = t.dark ? 'rgba(124,155,255,0.30)' : 'rgba(61,107,245,0.20)';
  const line = t.dark ? 'rgba(124,155,255,0.55)' : 'rgba(61,107,245,0.45)';

  return (
    <Svg width={size} height={size} viewBox="0 0 120 120" fill="none">
      <Defs>
        <LinearGradient id="libA" x1="0" y1="0" x2="1" y2="1">
          <Stop offset="0" stopColor={c2} />
          <Stop offset="1" stopColor={c1} />
        </LinearGradient>
      </Defs>
      {/* 后面两张倾斜的纸 */}
      <Rect x="34" y="30" width="46" height="58" rx="7" fill={c1} transform="rotate(-9 57 59)" />
      <Rect x="40" y="32" width="46" height="58" rx="7" fill={c1} transform="rotate(7 63 61)" />
      {/* 前面一张 */}
      <Rect x="38" y="34" width="46" height="58" rx="7" fill={t.card} stroke={line} strokeWidth="1.5" />
      {/* 纸上的横线 */}
      <Path d="M48 50h26M48 58h26M48 66h16" stroke={line} strokeWidth="1.6" strokeLinecap="round" opacity={0.7} />
      {/* 右上角的星 */}
      <G>
        <Path
          d="M88 26l2.6 6.4 6.4 2.6-6.4 2.6L88 44l-2.6-6.4L79 35l6.4-2.6z"
          fill={c2}
        />
      </G>
    </Svg>
  );
}

/** 目标空态：一面旗 + 一条虚线路径。 */
export function EmptyGoals({ size = 120 }: { size?: number }) {
  const t = useTheme();
  const c1 = t.dark ? 'rgba(124,155,255,0.16)' : 'rgba(61,107,245,0.10)';
  const c2 = t.dark ? 'rgba(124,155,255,0.32)' : 'rgba(61,107,245,0.22)';
  const line = t.dark ? 'rgba(124,155,255,0.55)' : 'rgba(61,107,245,0.45)';

  return (
    <Svg width={size} height={size} viewBox="0 0 120 120" fill="none">
      <Defs>
        <LinearGradient id="goalA" x1="0" y1="0" x2="1" y2="1">
          <Stop offset="0" stopColor={c2} />
          <Stop offset="1" stopColor={c1} />
        </LinearGradient>
      </Defs>
      {/* 虚线路径 */}
      <Path
        d="M26 92c10-6 16-18 12-28"
        stroke={line}
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeDasharray="4 5"
        opacity={0.6}
      />
      {/* 旗杆 */}
      <Path d="M64 88V32" stroke={line} strokeWidth="2.2" strokeLinecap="round" />
      {/* 旗面 */}
      <Path d="M64 32l26 9-26 9z" fill="url(#goalA)" />
      {/* 地面 */}
      <Circle cx="64" cy="92" r="12" fill={c1} />
      <Path d="M52 92h24" stroke={line} strokeWidth="1.6" strokeLinecap="round" opacity={0.5} />
    </Svg>
  );
}

/** 连接器空态：两个节点 + 一条连线。 */
export function EmptyConnectors({ size = 120 }: { size?: number }) {
  const t = useTheme();
  const c1 = t.dark ? 'rgba(124,155,255,0.16)' : 'rgba(61,107,245,0.10)';
  const c2 = t.dark ? 'rgba(124,155,255,0.32)' : 'rgba(61,107,245,0.22)';
  const line = t.dark ? 'rgba(124,155,255,0.55)' : 'rgba(61,107,245,0.45)';

  return (
    <Svg width={size} height={size} viewBox="0 0 120 120" fill="none">
      <Defs>
        <LinearGradient id="connA" x1="0" y1="0" x2="1" y2="1">
          <Stop offset="0" stopColor={c2} />
          <Stop offset="1" stopColor={c1} />
        </LinearGradient>
      </Defs>
      <Path
        d="M44 60h32"
        stroke={line}
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeDasharray="5 6"
        opacity={0.65}
      />
      <Circle cx="36" cy="60" r="14" fill="url(#connA)" />
      <Circle cx="36" cy="60" r="5" fill={t.card} stroke={line} strokeWidth="1.6" />
      <Circle cx="84" cy="60" r="14" fill="url(#connA)" />
      <Circle cx="84" cy="60" r="5" fill={t.card} stroke={line} strokeWidth="1.6" />
    </Svg>
  );
}

/** 对话空态：三个叠起来的对话气泡。 */
export function EmptyChat({ size = 132 }: { size?: number }) {
  const t = useTheme();
  const c1 = t.dark ? 'rgba(124,155,255,0.14)' : 'rgba(61,107,245,0.08)';
  const c2 = t.dark ? 'rgba(124,155,255,0.28)' : 'rgba(61,107,245,0.18)';
  const line = t.dark ? 'rgba(124,155,255,0.5)' : 'rgba(61,107,245,0.4)';

  return (
    <Svg width={size} height={size} viewBox="0 0 132 132" fill="none">
      <Defs>
        <LinearGradient id="chatA" x1="0" y1="0" x2="1" y2="1">
          <Stop offset="0" stopColor={c2} />
          <Stop offset="1" stopColor={c1} />
        </LinearGradient>
      </Defs>
      {/* 后排 */}
      <Path d="M78 34h30a8 8 0 018 8v18a8 8 0 01-8 8h-4l-2 9-9-9h-15a8 8 0 01-8-8V42a8 8 0 018-8z" fill={c1} opacity={0.75} />
      {/* 前排 */}
      <Path
        d="M28 52h44a10 10 0 0110 10v22a10 10 0 01-10 10H46l-10 11-1-11h-7a10 10 0 01-10-10V62a10 10 0 0110-10z"
        fill="url(#chatA)"
        stroke={line}
        strokeWidth="1.4"
      />
      {/* 气泡里的三点 */}
      <Circle cx="42" cy="73" r="3" fill={line} opacity={0.8} />
      <Circle cx="53" cy="73" r="3" fill={line} opacity={0.8} />
      <Circle cx="64" cy="73" r="3" fill={line} opacity={0.8} />
    </Svg>
  );
}
