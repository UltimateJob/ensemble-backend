import React, { useCallback, useEffect, useRef } from 'react';
import {
  Animated,
  PanResponder,
  StyleSheet,
  View,
  useWindowDimensions,
  type StyleProp,
  type ViewStyle,
} from 'react-native';

/**
 * 三页横滑。
 *
 * 为什么不用 react-native-pager-view：
 *   它是原生组件，需要额外配置，且我们只有三页、动效要求很轻。
 *   自己用 PanResponder + Animated 做，行为完全可控，也没有原生依赖。
 *
 * 手势：
 *   - 左滑 → 下一屏（index+1）
 *   - 右滑 → 上一屏（index-1）
 *   - 只在水平位移明显大于垂直、且超过阈值时才翻页——
 *     否则聊天记录上下滚动会被误判成翻页。
 *
 * 页面是常驻挂载的（不是懒加载），因为对话页有 SSE 流，
 * 切走再切回来不能断。
 */
export function Pager({
  index,
  onIndexChange,
  children,
  style,
}: {
  index: number;
  onIndexChange: (i: number) => void;
  children: React.ReactNode[];
  style?: StyleProp<ViewStyle>;
}) {
  const { width } = useWindowDimensions();
  const n = children.length;
  const translateX = useRef(new Animated.Value(-index * width)).current;
  const current = useRef(index);
  const dragStart = useRef(0);
  const dragging = useRef(false);

  // 外部 index 变化要同步。
  //
  // 首帧 useWindowDimensions 可能还没量出宽度（0），此时 initial 值算成 0，
  // 页面会停在第一屏——所以这里不能只判断 index 变了才同步，
  // 还要在 width 从 0 变成真实值时强制落位一次。
  const lastWidth = useRef(0);
  const didMount = useRef(false);
  useEffect(() => {
    const widthChanged = lastWidth.current !== width;
    lastWidth.current = width;

    // 首次挂载或宽度变化：直接落位到当前 index，不做动画。
    // 之前只在 width 变化时才落位，若首帧 width 已是正确值
    // （Web 上常见），就会停在 index=0 那一页。
    if (!didMount.current || widthChanged) {
      didMount.current = true;
      current.current = index;
      translateX.setValue(-index * width);
      return;
    }
    if (current.current === index) return;
    current.current = index;
    const to = -index * width;
    Animated.spring(translateX, {
      toValue: to,
      // marginLeft 是布局属性，用不了 native driver。
      // 代价是 JS 线程驱动，但这一处滑动本来就是低频交互，可接受。
      useNativeDriver: false,
      damping: 20,
      stiffness: 200,
      mass: 0.8,
    }).start();
  }, [index, width, translateX]);

  const settle = useCallback(
    (target: number) => {
      const clamped = Math.max(0, Math.min(n - 1, target));
      current.current = clamped;
      Animated.spring(translateX, {
        toValue: -clamped * width,
        useNativeDriver: false,
        damping: 20,
        stiffness: 200,
        mass: 0.8,
      }).start();
      if (clamped !== index) onIndexChange(clamped);
    },
    [n, width, index, onIndexChange, translateX],
  );

  const responder = useRef(
    PanResponder.create({
      // 只在横向明显时接管，纵向留给内部滚动
      onMoveShouldSetPanResponder: (_, g) =>
        Math.abs(g.dx) > 12 && Math.abs(g.dx) > Math.abs(g.dy) * 1.6,
      onPanResponderGrant: () => {
        dragging.current = true;
        dragStart.current = current.current;
        // @ts-expect-error 读取当前值
        translateX.setOffset(translateX._value);
        translateX.setValue(0);
      },
      onPanResponderMove: (_, g) => {
        // 边界回弹：第一页继续右拉、最后一页继续左拉时衰减
        let dx = g.dx;
        if ((current.current === 0 && dx > 0) || (current.current === n - 1 && dx < 0)) {
          dx *= 0.28;
        }
        translateX.setValue(dx);
      },
      onPanResponderRelease: (_, g) => {
        dragging.current = false;
        translateX.flattenOffset();
        const fast = Math.abs(g.vx) > 0.5;
        const far = Math.abs(g.dx) > width * 0.26;
        if (fast || far) {
          settle(dragStart.current + (g.dx < 0 ? 1 : -1));
        } else {
          settle(dragStart.current);
        }
      },
      onPanResponderTerminate: () => {
        dragging.current = false;
        translateX.flattenOffset();
        settle(current.current);
      },
    }),
  ).current;

  return (
    <View style={[styles.root, style]} {...responder.panHandlers}>
      {/*
        用 marginLeft 而不是 transform: translateX。
        RN Web 上 transform 只影响绘制，不影响布局与 hit-testing——
        元素视觉上移到了正确位置，但 DOM 仍在原处，
        导致点击落在了屏幕外（实测坐标 x=455 超出 390 视口）。
        用 marginLeft 会真正改变布局位置，点击才能命中。
      */}
      <Animated.View
        style={{
          flexDirection: 'row',
          width: width * n,
          flex: 1,
          marginLeft: translateX,
        }}
      >
        {children.map((child, i) => (
          <View key={i} style={{ width, flex: 1 }}>
            {child}
          </View>
        ))}
      </Animated.View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, overflow: 'hidden' },
});
