import { Platform } from 'react-native';
import * as SecureStore from 'expo-secure-store';

/**
 * 安全存储。
 *
 * 原生端用 expo-secure-store（iOS Keychain / Android Keystore），
 * **Web 端它不可用**——直接调用会抛异常，导致启动卡死在加载态。
 * Web 只是开发时的预览通道，退回 localStorage 即可；真机上
 * 令牌不会落到明文可读的存储里。
 */
const isWeb = Platform.OS === 'web';

export async function getItem(key: string): Promise<string | null> {
  if (isWeb) {
    try {
      return globalThis.localStorage?.getItem(key) ?? null;
    } catch {
      return null;
    }
  }
  try {
    return await SecureStore.getItemAsync(key);
  } catch {
    // 设备不支持（比如某些模拟器没配 Keychain）时不要崩，
    // 当作「没有存过」处理，让用户重新配对而不是白屏。
    return null;
  }
}

export async function setItem(key: string, value: string): Promise<void> {
  if (isWeb) {
    try {
      globalThis.localStorage?.setItem(key, value);
    } catch {
      /* 隐私模式下可能失败，忽略 */
    }
    return;
  }
  try {
    await SecureStore.setItemAsync(key, value);
  } catch {
    /* 同上：失败不阻断主流程 */
  }
}

export async function deleteItem(key: string): Promise<void> {
  if (isWeb) {
    try {
      globalThis.localStorage?.removeItem(key);
    } catch {
      /* ignore */
    }
    return;
  }
  try {
    await SecureStore.deleteItemAsync(key);
  } catch {
    /* ignore */
  }
}

/**
 * 清掉可再生的本地数据（当前群、默认群指针），**保留**账号/设备令牌与服务器地址。
 *
 * 「清除缓存」的语义就是「不影响登录、下次打开能自动重建」，
 * 所以令牌和地址必须留着——清了等于把用户踢下线。
 */
export async function clearCache(): Promise<void> {
  await deleteItem('zhituan_current_conv');
  await deleteItem('zhituan_default_conv');
}

/** 本地数据的大致体积，用于在设置页显示真实大小。 */
export async function cacheSize(): Promise<number> {
  let bytes = 0;
  for (const k of ['zhituan_current_conv', 'zhituan_default_conv']) {
    const v = await getItem(k);
    if (v) bytes += k.length + v.length;
  }
  return bytes;
}
