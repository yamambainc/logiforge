import { StrictMode, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { OfflineBanner } from './OfflineBanner';
import { start } from './auth';
import { Setup } from './screens/Setup';
import { registerServiceWorker } from './pwa';
import { boot } from './setup-boot';
import './styles.css';

const render = (n: ReactNode) => createRoot(document.getElementById('root')!).render(<StrictMode><OfflineBanner />{n}</StrictMode>);

registerServiceWorker();

// セットアップが終わっていなければ、サインインの前にセットアップの画面を出す（design/webui/signin-flow.md）
void boot(start).then((b) => render(b.kind === 'setup' ? <Setup initial={b.state} /> : <App />));
