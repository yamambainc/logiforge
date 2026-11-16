import { describe, expect, it } from 'vitest';
import { Problem, withPendingRetry } from './api';

describe('取り込み中の読み直し', () => {
  it('2 秒から倍にして、最大 30 秒の間隔で読み直す', async () => {
    let t = 0, calls = 0;
    const waits: number[] = [];
    const v = await withPendingRetry(async () => { if (++calls < 6) throw new Problem(503, 'snapshot_pending', ''); return 'ok'; }, () => {}, () => t, async (ms) => { waits.push(ms); t += ms; });
    expect(v).toBe('ok');
    expect(waits).toEqual([2000, 4000, 8000, 16000, 30000]);
  });
  it('5 分たったら諦める', async () => {
    let t = 0;
    await expect(withPendingRetry(async () => { throw new Problem(503, 'snapshot_pending', ''); }, () => {}, () => t, async (ms) => { t += ms; })).rejects.toMatchObject({ code: 'snapshot_pending_timeout' });
  });
  it('ほかのエラーは、そのまま返す', async () => {
    await expect(withPendingRetry(async () => { throw new Problem(404, 'not_found', ''); }, () => {}, () => 0, async () => {})).rejects.toMatchObject({ code: 'not_found' });
  });
});
