import { describe, expect, it } from 'vitest';
import { buildTree, isInactive, validVerdicts } from '../../../src/core/files/tree.js';
import { serializeHypothesis, serializeStory, serializeVerdict } from '../../../src/core/files/layout.js';

const H0 = 'h-0000000000';
const S1 = 's-1111111111';
const H1 = 'h-2222222222';
const V1 = 'v-3333333333';
const V2 = 'v-4444444444';

function files(entries: Record<string, Buffer | string>): Map<string, Buffer> {
  return new Map(Object.entries(entries).map(([k, v]) => [k, Buffer.isBuffer(v) ? v : Buffer.from(v)]));
}

const base = {
  [`tree/${H0}/hypothesis.md`]: serializeHypothesis({ id: H0, statement: '根' }),
  [`tree/${H0}/background.md`]: '背景',
  [`tree/${H0}/${S1}/story.md`]: serializeStory({ id: S1, title: 'T', summary: 'S' }),
  [`tree/${H0}/${S1}/${H1}/hypothesis.md`]: serializeHypothesis({ id: H1, statement: '子' }),
};

describe('buildTree', () => {
  it('交互に並ぶ木を組み立てる', () => {
    const t = buildTree(files(base));
    expect(t.errors).toEqual([]);
    expect(t.roots.map((r) => r.id)).toEqual([H0]);
    const h1 = t.hypotheses.get(H1)!;
    expect(h1.depth).toBe(2);
    expect(h1.parent?.id).toBe(S1);
    expect(t.hypotheses.get(H0)!.background).toBe('背景');
  });

  it('仮説の子にストーリーでないディレクトリがあれば、誤りとして集める', () => {
    const t = buildTree(files({ ...base, [`tree/${H0}/${H1}/hypothesis.md`]: serializeHypothesis({ id: H1, statement: 'x' }) }));
    expect(t.errors.join('\n')).toMatch(/ストーリーでない/);
  });

  it('決まっていないファイルを誤りとして集める', () => {
    const t = buildTree(files({ ...base, [`tree/${H0}/value.md`]: 'true' }));
    expect(t.errors.join('\n')).toMatch(/value\.md: 決まっていないファイル/);
  });

  it('id がディレクトリ名と違えば誤り', () => {
    const t = buildTree(files({ ...base, [`tree/${H0}/${S1}/story.md`]: serializeStory({ id: 's-9999999999', title: 'T', summary: 'S' }) }));
    expect(t.errors.join('\n')).toMatch(/id がディレクトリ名と違う/);
  });

  it('inactive は祖先のストーリーの inactivated.md で決まる', () => {
    const t = buildTree(files({ ...base, [`tree/${H0}/${S1}/inactivated.md`]: '理由' }));
    expect(t.stories.get(S1)!.inactivatedReason).toBe('理由');
    expect(isInactive(t.hypotheses.get(H1)!)).toBe(true);
    expect(isInactive(t.hypotheses.get(H0)!)).toBe(false);
  });

  it('有効な検証結果は supersedes に書かれていないもの', () => {
    const v = (id: string, supersedes: string | null) =>
      serializeVerdict({ id, value: true, supersedes, artifacts: [], citations: [], report: 'r' });
    const t = buildTree(
      files({
        ...base,
        [`tree/${H0}/${S1}/${H1}/verdicts/${V1}.md`]: v(V1, null),
        [`tree/${H0}/${S1}/${H1}/verdicts/${V2}.md`]: v(V2, V1),
      }),
    );
    expect(t.errors).toEqual([]);
    expect(validVerdicts(t.hypotheses.get(H1)!).map((x) => x.id)).toEqual([V2]);
    expect(t.verdicts.get(V1)!.hypothesis.id).toBe(H1);
  });
});
