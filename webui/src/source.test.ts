import { afterEach, describe, expect, it, vi } from 'vitest';
import { Problem } from './api';

const api = vi.hoisted(() => ({ getApplyRequest: vi.fn(), listNodes: vi.fn() }));
vi.mock('./api', async (orig) => ({ ...(await orig<typeof import('./api')>()), getApplyRequest: api.getApplyRequest, listNodes: api.listNodes }));
const { loadSource, isBlocked } = await import('./source');

const ar = { id: 'apr_1', state: 'open', branch: 'b', target: 'main', head_commit: 'c' };
afterEach(() => { api.getApplyRequest.mockReset(); api.listNodes.mockReset(); });

// 重ねられないときは、理由を示す（design/webui/overlay.md の表）
describe('重ねられないとき', () => {
  it('open でない', async () => {
    api.getApplyRequest.mockResolvedValue({ ...ar, state: 'merged' });
    const r = await loadSource('q', { kind: 'ar', id: 'apr_1' }, () => {});
    expect(isBlocked(r) && r.reason).toBe('not-open');
  });
  it('衝突。detail にパスがある', async () => {
    api.getApplyRequest.mockResolvedValue(ar);
    api.listNodes.mockRejectedValue(new Problem(409, 'overlay_conflict', 'tree/a.md'));
    const r = await loadSource('q', { kind: 'ar', id: 'apr_1' }, () => {});
    expect(isBlocked(r) && [r.reason, r.detail]).toEqual(['conflict', 'tree/a.md']);
  });
  it('ブランチが消えた、AR がない', async () => {
    api.getApplyRequest.mockRejectedValue(new Problem(404, 'not_found', ''));
    const r = await loadSource('q', { kind: 'ar', id: 'apr_1' }, () => {});
    expect(isBlocked(r) && r.reason).toBe('gone');
  });
  it('重ねる途中で open でなくなった', async () => {
    api.getApplyRequest.mockResolvedValue(ar);
    api.listNodes.mockRejectedValue(new Problem(409, 'not_open', ''));
    const r = await loadSource('q', { kind: 'ar', id: 'apr_1' }, () => {});
    expect(isBlocked(r) && r.reason).toBe('not-open');
  });
});
