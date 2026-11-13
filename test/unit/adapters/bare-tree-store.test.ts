import { describe, expect, it } from 'vitest';
import { BareGitTreeStore } from '../../../src/adapters/git/bare-tree-store.js';
import { useTempDir } from '../../helpers/tmp.js';

const tempDir = useTempDir();
const f = (path: string, content: string) => ({ path, content: Buffer.from(content) });

async function setup() {
  const store = new BareGitTreeStore(await tempDir());
  await store.prepare('demo');
  const root = await store.createInitialMain('demo', [f('tree/h-0000000000/hypothesis.md', 'root')], 'init\n\nLogiforge-Signature: x\n');
  return { store, root };
}

describe('BareGitTreeStore', () => {
  it('用意した repository を列挙する。用意は何度呼んでもよい', async () => {
    const store = new BareGitTreeStore(await tempDir());
    expect(await store.list()).toEqual([]);
    await store.prepare('b-repo');
    await store.prepare('a-repo');
    await store.prepare('a-repo');
    expect(await store.list()).toEqual(['a-repo', 'b-repo']);
    expect(await store.readMain('a-repo')).toBeNull();
  });

  it('repository の名前の形が違えば拒む', async () => {
    const store = new BareGitTreeStore(await tempDir());
    await expect(store.prepare('../evil')).rejects.toThrow();
  });

  it('最初のリビジョンで main を作る。main があればもう作れない', async () => {
    const { store, root } = await setup();
    expect(await store.readMain('demo')).toBe(root);
    const info = await store.readRevision('demo', root);
    expect(info?.parents).toEqual([]);
    expect(info?.message).toContain('Logiforge-Signature: x');
    await expect(store.createInitialMain('demo', [f('a', 'b')], 'again')).rejects.toThrow();
  });

  it('リビジョンを作っても main は動かず、名前で読める', async () => {
    const { store, root } = await setup();
    const rev = await store.createRevision('demo', root, [f('tree/h-0000000000/background.md', 'bg')], 'v1', 'apply-requests/apr_1/1');
    expect(await store.readMain('demo')).toBe(root);
    expect(await store.readNamed('demo', 'apply-requests/apr_1/1')).toBe(rev);
    expect(await store.readNamed('demo', 'apply-requests/apr_1/2')).toBeNull();
    const files = await store.readFiles('demo', rev, 'tree');
    expect([...files.keys()].sort()).toEqual(['tree/h-0000000000/background.md', 'tree/h-0000000000/hypothesis.md']);
    expect((await store.readFile('demo', rev, 'tree/h-0000000000/background.md'))?.toString()).toBe('bg');
    expect(await store.readFile('demo', root, 'tree/h-0000000000/background.md')).toBeNull();
  });

  it('バイナリのファイルをそのまま読める', async () => {
    const { store, root } = await setup();
    const bin = Buffer.from([0, 1, 2, 10, 255, 10, 0]);
    const rev = await store.createRevision('demo', root, [{ path: 'artifacts/sha256/aa/b', content: bin }], 'm', 'x');
    expect(await store.readFile('demo', rev, 'artifacts/sha256/aa/b')).toEqual(bin);
  });

  it('main を進めるのは、期待する親が今の main のときだけ', async () => {
    const { store, root } = await setup();
    const a = await store.createRevision('demo', root, [f('tree/a', '1')], 'a', 'a');
    const b = await store.createRevision('demo', root, [f('tree/b', '1')], 'b', 'b');
    expect(await store.advanceMain('demo', root, a)).toBe(true);
    expect(await store.advanceMain('demo', root, b)).toBe(false);
    expect(await store.readMain('demo')).toBe(a);
    expect(await store.mergeBase('demo', a, b)).toBe(root);
    expect(await store.isAncestor('demo', root, a)).toBe(true);
    expect(await store.isAncestor('demo', b, a)).toBe(false);
  });

  it('変更を列挙する・リビジョンの並びを読む', async () => {
    const { store, root } = await setup();
    const a = await store.createRevision('demo', root, [f('tree/x/one', '1')], 'a', 'a');
    await store.advanceMain('demo', root, a);
    const b = await store.createRevision('demo', a, [f('tree/y/two', '1')], 'b', 'b');
    await store.advanceMain('demo', a, b);
    expect((await store.listMainRevisions('demo')).map((r) => r.revision)).toEqual([root, a, b]);
    expect((await store.listChanges('demo', root, b, ['tree/x'])).map((r) => r.revision)).toEqual([a]);
    expect((await store.listChanges('demo', a, b, ['tree/x'])).map((r) => r.revision)).toEqual([]);
    expect((await store.listChanges('demo', null, b, ['tree/y/two', 'tree/x'])).map((r) => r.revision)).toEqual([a, b]);
  });

  it('知らないリビジョンは null を返す', async () => {
    const { store } = await setup();
    expect(await store.readRevision('demo', 'f'.repeat(40))).toBeNull();
    expect(await store.readRevision('demo', '--help')).toBeNull();
    expect((await store.readFiles('demo', 'nope', 'tree')).size).toBe(0);
  });
});
