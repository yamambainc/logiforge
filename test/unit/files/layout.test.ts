import { describe, expect, it } from 'vitest';
import {
  artifactPath,
  parseAnswer,
  parseHypothesis,
  parseStory,
  parseVerdict,
  serializeAnswer,
  serializeHypothesis,
  serializeStory,
  serializeVerdict,
  sha256FromArtifactPath,
  sha256Hex,
} from '../../../src/core/files/layout.js';

describe('ファイルの中身', () => {
  it('hypothesis.md を書いて読める', () => {
    const c = { id: 'h-0123456789', statement: '主文: 「引用」\nを含む' };
    expect(parseHypothesis(serializeHypothesis(c).toString(), 'p')).toEqual(c);
  });

  it('story.md を書いて読める', () => {
    const c = { id: 's-0123456789', title: 'タイトル', summary: '概要' };
    expect(parseStory(serializeStory(c).toString(), 'p')).toEqual(c);
  });

  it('検証結果を書いて読める。最初の検証結果は supersedes を書かない', () => {
    const c = {
      id: 'v-0123456789',
      value: true,
      supersedes: null,
      artifacts: [{ kind: 'link' as const, name: 'paper', uri: 'https://example.com/a' }],
      citations: [{ target_kind: 'subtree' as const, node_id: 'h-abcdefghjk', revision: 'a'.repeat(40) }],
      report: '# レポート\n\n本文\n',
    };
    const text = serializeVerdict(c).toString();
    expect(text).not.toContain('supersedes');
    expect(parseVerdict(text, 'p')).toEqual(c);
  });

  it('回答のファイルを書いて読める', () => {
    const c = {
      human_request_id: 'hrq_01J',
      answered_by: 'alice',
      answered_at: '2026-11-13T00:00:00.000Z',
      attachments: [],
      body: '測った結果は 3 cm だった',
    };
    expect(parseAnswer(serializeAnswer(c).toString(), 'p')).toEqual(c);
  });

  it('決まっていない項目を拒む', () => {
    expect(() => parseHypothesis('---\nid: h-0123456789\nstatement: a\nvalue: true\n---\n', 'p')).toThrow(/value/);
  });

  it('artifact のパスと sha256 を行き来できる', () => {
    const sha = sha256Hex(Buffer.from('x'));
    const path = artifactPath(sha);
    expect(path).toBe(`artifacts/sha256/${sha.slice(0, 2)}/${sha.slice(2)}`);
    expect(sha256FromArtifactPath(path)).toBe(sha);
    expect(sha256FromArtifactPath('artifacts/sha256/zz/1')).toBeNull();
  });
});
