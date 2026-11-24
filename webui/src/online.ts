// つながっているか（design/webui/pwa.md の「つながっていないとき」）。navigator.onLine と、online・offline のイベントを使う。
// onLine が true でも、つながるとは限らない。false なら、つながっていない。そのため、false のときだけ使う。

/** 端末がネットにつながっていないと分かっているか。 */
export const isOffline = () => typeof navigator !== 'undefined' && navigator.onLine === false;

/** つながったら、1 度だけ f を呼ぶ。 */
export function onceOnline(f: () => void) {
  window.addEventListener('online', f, { once: true });
}

/** つながる・切れるたびに f を呼ぶ。止める関数を返す。 */
export function subscribeOnline(f: () => void): () => void {
  window.addEventListener('online', f);
  window.addEventListener('offline', f);
  return () => { window.removeEventListener('online', f); window.removeEventListener('offline', f); };
}
