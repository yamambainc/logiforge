import { describe, expect, it } from 'vitest';
import { base64Len, checkAttachments, isWebLink, MAX_FILE, toBase64 } from './files';

// 回答の添付の大きさ（design/api/README.md の「アップロードの大きさ」：ファイルは 10 MiB まで、要求は 32 MiB まで）
describe('回答の添付', () => {
  it('base64 の長さ', () => {
    expect(base64Len(0)).toBe(0);
    expect(base64Len(1)).toBe(4);
    expect(base64Len(3)).toBe(4);
    expect(base64Len(4)).toBe(8);
    expect(toBase64(new TextEncoder().encode('rate,0.42\n').buffer as ArrayBuffer)).toBe(btoa('rate,0.42\n'));
  });
  it('1 つのファイルは 10 MiB まで', () => {
    expect(checkAttachments('a', [{ name: 'x', size: MAX_FILE }], [])).toBeNull();
    expect(checkAttachments('a', [{ name: 'x', size: MAX_FILE + 1 }], [])).toMatch('10 MiB');
  });
  it('10 MiB を 2 つは送れ、3 つは 32 MiB を超える', () => {
    const f = { name: 'x', size: MAX_FILE };
    expect(checkAttachments('a', [f, f], [])).toBeNull();
    expect(checkAttachments('a', [f, f, f], [])).toMatch('32 MiB');
  });
  it('リンクは http か https', () => {
    expect(checkAttachments('a', [], [{ name: 'l', uri: 'https://example.com' }])).toBeNull();
    expect(checkAttachments('a', [], [{ name: 'l', uri: 'javascript:alert(1)' }])).toMatch('http か https');
    // URL として読み、protocol で確かめる（大文字や空白で、正規表現をすり抜けない）
    expect(isWebLink('HTTPS://example.com/a')).toBe(true);
    expect(isWebLink(' javascript:alert(1)')).toBe(false);
    expect(isWebLink('https//example.com')).toBe(false);
    expect(isWebLink(undefined)).toBe(false);
  });
});
