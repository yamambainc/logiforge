import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { abortAfter } from './api';

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });

// AbortSignal.any と AbortSignal.timeout がない環境（古い iOS の Safari）を真似る
describe('要求をやめる', () => {
  const any = AbortSignal.any, timeout = AbortSignal.timeout;
  beforeEach(() => { Object.assign(AbortSignal, { any: undefined, timeout: undefined }); });
  afterEach(() => { Object.assign(AbortSignal, { any, timeout }); });

  it('時間が来たら、やめる', () => {
    const { signal } = abortAfter(new AbortController().signal, 20_000);
    vi.advanceTimersByTime(19_999);
    expect(signal.aborted).toBe(false);
    vi.advanceTimersByTime(1);
    expect(signal.aborted).toBe(true);
  });
  it('親でやめたら、すぐやめる', () => {
    const parent = new AbortController();
    const { signal } = abortAfter(parent.signal, 20_000);
    parent.abort();
    expect(signal.aborted).toBe(true);
  });
  it('親がやめてあれば、はじめからやめている', () => {
    const parent = new AbortController();
    parent.abort();
    expect(abortAfter(parent.signal, 20_000).signal.aborted).toBe(true);
  });
  it('done のあとは、時間が来てもやめない', () => {
    const { signal, done } = abortAfter(new AbortController().signal, 20_000);
    done();
    vi.advanceTimersByTime(30_000);
    expect(signal.aborted).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });
});
