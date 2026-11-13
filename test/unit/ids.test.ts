import { describe, expect, it } from 'vitest';
import { HYPOTHESIS_ID_PATTERN, newNodeId, nodeKindOf } from '../../src/core/ids.js';

describe('newNodeId', () => {
  it('種類の接頭辞と 10 文字の Crockford base32 をつなぐ', () => {
    for (let i = 0; i < 200; i++) {
      const id = newNodeId('h');
      expect(id).toMatch(HYPOTHESIS_ID_PATTERN);
      expect(nodeKindOf(id)).toBe('h');
    }
  });

  it('i、l、o、u を使わない', () => {
    const ids = Array.from({ length: 500 }, () => newNodeId('s').slice(2)).join('');
    expect(ids).not.toMatch(/[ilou]/);
  });

  it('形の違う識別子は種類を持たない', () => {
    expect(nodeKindOf('h-short')).toBeNull();
    expect(nodeKindOf('x-0123456789')).toBeNull();
  });
});
