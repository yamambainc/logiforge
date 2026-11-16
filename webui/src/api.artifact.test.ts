import { afterEach, describe, expect, it, vi } from 'vitest';

const onUnauthorized = vi.fn();
vi.mock('./auth', () => ({
  getAuth: () => ({ kind: 'signed-in', token: 't', expiresAt: Date.now() + 3600_000 }),
  markVerified: () => {},
  onUnauthorized: () => onUnauthorized(),
}));
const { fetchArtifact } = await import('./api');

afterEach(() => { vi.unstubAllGlobals(); onUnauthorized.mockClear(); });

describe('artifact のファイル', () => {
  it('401 なら、サインインに渡す', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('', { status: 401 })));
    await expect(fetchArtifact('q', 'abc')).rejects.toMatchObject({ status: 401 });
    expect(onUnauthorized).toHaveBeenCalledOnce();
  });
  it('アクセストークンを付けて読む', async () => {
    const f = vi.fn(async (_u: string, _i: RequestInit) => new Response('x', { status: 200 }));
    vi.stubGlobal('fetch', f);
    expect(await (await fetchArtifact('q', 'abc')).text()).toBe('x');
    expect(f.mock.calls[0][1].headers).toEqual({ Authorization: 'Bearer t' });
    expect(onUnauthorized).not.toHaveBeenCalled();
  });
});
