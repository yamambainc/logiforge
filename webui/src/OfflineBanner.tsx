import { useSyncExternalStore } from 'react';
import { isOffline, subscribeOnline } from './online';

// つながっていないときの知らせ（design/webui/pwa.md の「つながっていないとき」）。画面の上に重ねて出す。
export function OfflineBanner() {
  const offline = useSyncExternalStore(subscribeOnline, isOffline, () => false);
  if (!offline) return null;
  return <div className="offline-banner" role="status">つながっていません。つながると、もう一度読み込めます。</div>;
}
