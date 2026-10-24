import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import React, { useEffect, useState } from 'react';
import { ActivityIndicator, Platform, View } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { ErrorBoundary } from '../src/components/ErrorBoundary';
import { loadBaseURL } from '../src/lib/api';
import { boot } from '../src/lib/session';
import { useApp } from '../src/lib/store';
import { useTheme } from '../src/theme/theme';
import { useFonts } from 'expo-font';
import { FONT_ASSETS } from '../src/theme/fonts';

export default function RootLayout() {
  const t = useTheme();
  const ready = useApp((s) => s.ready);
  const [phase, setPhase] = useState<'booting' | 'done'>('booting');
  // 字体加载失败不阻塞启动——退回系统字体，界面仍然可用
  const [fontsLoaded] = useFonts(FONT_ASSETS);

  // Web 预览专用：去掉浏览器给输入框加的黑色焦点描边。
  // App 里没有这个东西，出现在网页版会显得很粗糙。
  useEffect(() => {
    if (Platform.OS !== 'web') return;
    const style = document.createElement('style');
    style.textContent =
      'input,textarea,[contenteditable]{outline:none !important;box-shadow:none !important}' +
      'input:focus,textarea:focus,[contenteditable]:focus{outline:none !important;box-shadow:none !important}' +
      '::-webkit-scrollbar{width:0;height:0}';
    document.head.appendChild(style);
    return () => {
      document.head.removeChild(style);
    };
  }, []);

  useEffect(() => {
    (async () => {
      try {
        const base = await loadBaseURL();
        if (base) await boot();
      } catch {
        // 启动失败不该把用户永久卡在转圈上——回到入口页可改地址或重配对
        useApp.getState().setReady(false);
      } finally {
        setPhase('done');
      }
    })();
  }, []);

  if (phase === 'booting' || !fontsLoaded) {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: t.canvas[1] }}>
        <ActivityIndicator color={t.accent} />
      </View>
    );
  }

  return (
    <SafeAreaProvider>
      <StatusBar style={t.dark ? 'light' : 'dark'} />
      <ErrorBoundary>
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: t.canvas[1] },
            animation: 'slide_from_right',
          }}
        >
          <Stack.Screen name="index" options={{ animation: 'fade' }} />
          <Stack.Protected guard={ready}>
            <Stack.Screen name="(main)" options={{ animation: 'fade' }} />
            <Stack.Screen name="team/[id]" />
            <Stack.Screen name="team/new" options={{ presentation: 'modal', animation: 'slide_from_bottom' }} />
            <Stack.Screen name="goals" />
            <Stack.Screen name="library" />
            <Stack.Screen name="artifact/[id]" />
            <Stack.Screen name="connect" />
            <Stack.Screen name="settings/index" />
            {/* 之前 solo / team/task / usage 没登记在这里，
                等于没登录也能被深链直接打开。 */}
            <Stack.Screen name="solo" />
            <Stack.Screen name="team/task" />
            <Stack.Screen name="usage" />
          </Stack.Protected>
        </Stack>
      </ErrorBoundary>
    </SafeAreaProvider>
  );
}
