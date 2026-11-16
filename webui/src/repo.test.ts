import { describe, expect, it } from 'vitest';
import { match, screenPath } from './router';
import { charCount, validateNew } from './screens/RepositoryList';

describe('URL と画面', () => {
  it('repository の中の画面', () => {
    expect(match('/r/q')).toEqual({ name: 'dashboard', repo: 'q' });
    expect(match('/r/q/tree')).toEqual({ name: 'tree', repo: 'q' });
    expect(match('/r/q/nothing')).toEqual({ name: 'unknown' });
    expect(match('/')).toEqual({ name: 'repositories' });
    expect(screenPath('q', 'dashboard')).toBe('/r/q');
    expect(screenPath('q', 'tree')).toBe('/r/q/tree');
  });
});

describe('repository の作成', () => {
  it('主文の文字数は、NFC に正規化したあとのコードポイント数', () => {
    expect(charCount('が')).toBe(1);
    expect(charCount('が')).toBe(1); // 濁点を分けて書いても 1 字
    expect(charCount('😀')).toBe(1);
  });
  it('名前と主文を確かめる', () => {
    expect(validateNew('q-1', '問い')).toBeNull();
    expect(validateNew('-q', '問い')).toMatch('名前');
    expect(validateNew('Q', '問い')).toMatch('名前');
    expect(validateNew('q', '  ')).toMatch('主文を書いて');
    expect(validateNew('q', 'あ'.repeat(51))).toMatch('50 字以下');
    expect(validateNew('q', 'あ'.repeat(50))).toBeNull();
  });
});
