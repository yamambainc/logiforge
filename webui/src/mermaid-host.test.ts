import { afterEach, describe, expect, it, vi } from 'vitest';
import { acceptSvg, createHost, MAX_SVG, type Frame } from './mermaid-host';

// iframe の代わり。ready を送る口と、渡されたポートで答える口を持つ
function fakeFrame() {
  const win = { name: 'iframe の window' };
  let listener: ((e: { source: unknown; data: unknown }) => void) | null = null;
  const ports: MessagePort[] = [];
  const frames: Frame[] = [];
  const make = (): Frame => {
    const f: Frame = { window: win, sendPort: (p) => { ports.push(p); }, remove: () => {} };
    frames.push(f);
    return f;
  };
  const listen = (f: (e: { source: unknown; data: unknown }) => void) => { listener = f; return () => { listener = null; }; };
  const send = (source: unknown, data: unknown) => listener?.({ source, data });
  // ポートの向こうで、決めた答えを返す
  const answer = (port: MessagePort, reply: (code: string) => unknown) => {
    port.onmessage = (m: MessageEvent) => port.postMessage({ id: (m.data as { id: string }).id, ...(reply((m.data as { code: string }).code) as object) });
  };
  return { win, make, listen, send, ports, frames, answer };
}

const tick = () => new Promise((r) => setTimeout(r, 0));
const SVG = '<svg xmlns="http://www.w3.org/2000/svg"><g><text>A</text></g></svg>';

afterEach(() => { vi.useRealTimers(); });

describe('Mermaid の iframe とのやりとり', () => {
  it('置いた iframe の ready を受けたら、ポートを渡し、図の文と SVG はポートでやりとりする', async () => {
    const f = fakeFrame();
    const host = createHost(f.make, f.listen, 1000);
    const p = host.render('graph LR\n A-->B');
    await tick();
    f.send(f.win, { ready: true });
    await tick();
    expect(f.ports).toHaveLength(1);
    f.answer(f.ports[0], (code) => ({ svg: code.includes('A-->B') ? SVG : '' }));
    await expect(p).resolves.toBe(SVG);
  });

  it('ほかのフレームやウィンドウからの ready と、2 回目の ready（iframe が別の URL に移ったあと）は受けない', async () => {
    const f = fakeFrame();
    const host = createHost(f.make, f.listen, 1000);
    const p = host.render('graph LR\n A-->B');
    await tick();
    f.send({ name: 'ほかのフレーム' }, { ready: true });
    f.send(f.win, { ready: 'yes' });
    expect(f.ports).toHaveLength(0);
    f.send(f.win, { ready: true });
    await tick();
    // 移ったあとの文書が、また ready を送ってきても、新しいポートは渡さない
    f.send(f.win, { ready: true });
    expect(f.ports).toHaveLength(1);
    f.answer(f.ports[0], () => ({ svg: SVG }));
    await expect(p).resolves.toBe(SVG);
  });

  it('ready が来なければ、15 秒で諦める。次の図で、iframe を作り直す', async () => {
    vi.useFakeTimers();
    const f = fakeFrame();
    const host = createHost(f.make, f.listen);
    const p = host.render('graph LR\n A-->B');
    const check = expect(p).rejects.toThrow('読み込めませんでした');
    await vi.advanceTimersByTimeAsync(15_000);
    await check;
    void host.render('graph LR\n C-->D').catch(() => {});
    await vi.advanceTimersByTimeAsync(0);
    expect(f.frames).toHaveLength(2);
  });

  it('答えが来なければ、15 秒で諦める', async () => {
    vi.useFakeTimers();
    const f = fakeFrame();
    const host = createHost(f.make, f.listen);
    const p = host.render('graph LR\n A-->B');
    await vi.advanceTimersByTimeAsync(0);
    f.send(f.win, { ready: true });
    const check = expect(p).rejects.toThrow('時間内に描けませんでした');
    await vi.advanceTimersByTimeAsync(15_000);
    await check;
  });

  it('描けなかったことは、Error で返す', async () => {
    const f = fakeFrame();
    const host = createHost(f.make, f.listen, 1000);
    const p = host.render('graph ?');
    await tick();
    f.send(f.win, { ready: true });
    await tick();
    f.answer(f.ports[0], () => ({ error: 'Parse error on line 1' }));
    await expect(p).rejects.toThrow('Parse error');
  });
});

describe('受け取る SVG', () => {
  it('2 MiB を超えるものと、<svg で始まらないものは断る', () => {
    expect(acceptSvg(SVG)).toBe(true);
    expect(acceptSvg('<svg>' + 'a'.repeat(MAX_SVG))).toBe(false);
    expect(acceptSvg('<html><svg></svg></html>')).toBe(false);
    expect(acceptSvg(42)).toBe(false);
  });
  it('動くもの（script、on…=、javascript:、foreignObject）を含むものは断る', () => {
    for (const bad of ['<svg><script>alert(1)</script></svg>', '<svg><g onclick="x()"/></svg>', '<svg><a href="javascript:x()"/></svg>', '<svg><foreignObject><div/></foreignObject></svg>', '<svg><image ONLOAD = "x"/></svg>']) {
      expect(acceptSvg(bad), bad).toBe(false);
    }
  });
});
