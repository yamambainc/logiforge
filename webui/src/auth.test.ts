import { describe, expect, it } from 'vitest';
import { authorizeURL, base64url, codeChallenge, safeReturnTo } from './auth';

describe('PKCE', () => {
  it('RFC 7636 の付録 B の例と同じ challenge を作る', async () => {
    expect(await codeChallenge('dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk')).toBe('E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM');
  });
  it('base64url は = を付けず、+ と / を使わない', () => {
    expect(base64url(new Uint8Array([251, 255, 191]))).toBe('-_-_');
  });
});

describe('authorize の URL', () => {
  it('S256 と state とスコープを付ける', () => {
    const u = new URL(authorizeURL({ authorize_url: 'https://forge.example/login/oauth/authorize', client_id: 'c', redirect_uri: 'https://lf.example/oauth/callback', scope: 'read:user read:organization' }, 'st', 'ch'));
    expect(u.searchParams.get('code_challenge_method')).toBe('S256');
    expect(u.searchParams.get('state')).toBe('st');
    expect(u.searchParams.get('scope')).toBe('read:user read:organization');
    expect(u.searchParams.get('response_type')).toBe('code');
  });
});

describe('戻り先', () => {
  it('同じオリジンのパスだけを受け付ける', () => {
    expect(safeReturnTo('/r/q/tree?node=h-1')).toBe('/r/q/tree?node=h-1');
    expect(safeReturnTo('https://evil.example/')).toBe('/');
    expect(safeReturnTo('//evil.example/')).toBe('/');
    expect(safeReturnTo('/oauth/callback?code=x')).toBe('/');
    expect(safeReturnTo(undefined)).toBe('/');
  });
});
