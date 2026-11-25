import { describe, expect, it } from 'vitest';
import { route, type Req } from './sw-route';

const O = 'https://lf.example';
const get = (path: string, over: Partial<Req> = {}): Req => ({ url: O + path, method: 'GET', mode: 'cors', destination: '', ...over });
const nav = (path: string, destination = 'document') => get(path, { mode: 'navigate', destination });

describe('Service Worker の振り分け', () => {
  it('API とサインインの中継には触れない', () => {
    for (const p of ['/v1/repositories', '/v1/oauth/session', '/v1/oauth/token', '/v1/oauth/config', '/v1/setup', '/v1', '/healthz']) expect(route(get(p), O)).toBe('pass');
    expect(route(nav('/v1/repositories/q/artifacts/a'), O)).toBe('pass');
  });
  it('GET のほかと、ほかのオリジンには触れない', () => {
    expect(route(get('/assets/main-x.js', { method: 'POST' }), O)).toBe('pass');
    expect(route({ ...nav('/login/oauth/authorize'), url: 'https://forge.example/login/oauth/authorize' }, O)).toBe('pass');
  });
  it('画面の遷移（サインインの戻りを含む）は page。ページの中の文書（Mermaid の iframe）は触れない', () => {
    expect(route(nav('/'), O)).toBe('page');
    expect(route(nav('/r/q/tree?node=h-1'), O)).toBe('page');
    expect(route(nav('/oauth/callback?code=c&state=s'), O)).toBe('page');
    expect(route(nav('/mermaid.html', 'iframe'), O)).toBe('pass');
  });
  it('ハッシュ付きの JS と CSS は asset、アイコンと manifest は static、ほかは触れない', () => {
    expect(route(get('/assets/main-x.js'), O)).toBe('asset');
    expect(route(get('/icon-192.png'), O)).toBe('static');
    expect(route(get('/manifest.webmanifest'), O)).toBe('static');
    expect(route(get('/sw.js'), O)).toBe('pass');
    expect(route(get('/mermaid.html'), O)).toBe('pass');
  });
});
