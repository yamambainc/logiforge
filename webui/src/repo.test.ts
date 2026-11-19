import { describe, expect, it } from 'vitest';
import { match, screenPath } from './router';
import { charCount, validateName, validateStatement } from './settings';

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

// 主文の 50 字と数え方は、BE と同じ（docs/tree/README.md の「仮説」、internal/tree/check.go の MaxStatementChars と CharCount）
describe('repository の作成', () => {
  it('主文の文字数は、NFC に正規化したあとのコードポイント数', () => {
    expect(charCount('が')).toBe(1);
    expect(charCount('が')).toBe(1); // 濁点を分けて書いても 1 字
    expect(charCount('😀')).toBe(1);
  });
  it('名前と主文を確かめる', () => {
    expect(validateName('q-1')).toBeNull();
    expect(validateName('-q')).toMatch('名前');
    expect(validateName('Q')).toMatch('名前');
    expect(validateStatement('問い')).toBeNull();
    expect(validateStatement('  ')).toMatch('主文を書いて');
    expect(validateStatement('あ'.repeat(51))).toMatch('50 字以下');
    expect(validateStatement('あ'.repeat(50))).toBeNull();
  });
});
