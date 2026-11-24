// Service Worker を入れる（design/webui/pwa.md）。ビルドした WebUI でだけ入れる（開発の vite では入れない）。
export function registerServiceWorker() {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator)) return;
  // updateViaCache: 'none'：sw.js を HTTP のキャッシュから読まない。新しい版を配ったら、次に開いたときに入れ替わる
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/', updateViaCache: 'none' }).catch(() => { /* 入れられなくても、ふだんどおりに動く */ });
  });
}
