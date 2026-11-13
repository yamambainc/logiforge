/**
 * 木の読み書きのポートを、ローカルの bare リポジトリで実装する（design/adapters/git.md）。
 * git リポジトリのパスは、<baseDir>/<repository の名前>.git とする。
 */
import { mkdir, readdir, rename, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import type { FileChange, Revision, RevisionInfo, TreeStore } from '../../core/ports/tree-store.js';
import { REPOSITORY_NAME_PATTERN } from '../../core/ids.js';
import { GitError, runGit } from './run-git.js';

const REVISION_PATTERN = /^[0-9a-f]{40}([0-9a-f]{24})?$/;
const REF_NAME_PATTERN = /^[a-z0-9_-]+(\/[a-z0-9_-]+)*$/;
const MAIN_REF = 'refs/heads/main';
const NAMED_REF_PREFIX = 'refs/logiforge/';
const LOG_FORMAT = '%H%x00%T%x00%P%x00%ct%x00%B%x1e';

const IDENTITY = {
  GIT_AUTHOR_NAME: 'Logiforge',
  GIT_AUTHOR_EMAIL: 'logiforge@localhost',
  GIT_COMMITTER_NAME: 'Logiforge',
  GIT_COMMITTER_EMAIL: 'logiforge@localhost',
};

export class BareGitTreeStore implements TreeStore {
  constructor(private readonly baseDir: string) {}

  private dir(repository: string): string {
    if (!REPOSITORY_NAME_PATTERN.test(repository)) throw new Error(`repository の名前が正しくない: ${repository}`);
    return join(this.baseDir, `${repository}.git`);
  }

  private git(repository: string, args: string[], input?: Buffer | string, env?: Record<string, string>): Promise<Buffer> {
    return runGit(args, { cwd: this.dir(repository), input, env });
  }

  async prepare(repository: string): Promise<void> {
    if (await this.exists(repository)) return;
    const dir = this.dir(repository);
    await mkdir(this.baseDir, { recursive: true });
    const tmp = join(this.baseDir, `.tmp-${repository}-${randomUUID()}`);
    await mkdir(tmp);
    try {
      await runGit(['init', '--bare', '--quiet', '--initial-branch=main', tmp], { cwd: this.baseDir });
      // 作りかけのディレクトリを repository として列挙しないように、できてから名前を付ける
      await rename(tmp, dir);
    } catch (e) {
      await rm(tmp, { recursive: true, force: true });
      if (await this.exists(repository)) return;
      throw e;
    }
  }

  async list(): Promise<string[]> {
    let entries: string[];
    try {
      entries = await readdir(this.baseDir);
    } catch {
      return [];
    }
    return entries
      .filter((e) => e.endsWith('.git'))
      .map((e) => e.slice(0, -4))
      .filter((n) => REPOSITORY_NAME_PATTERN.test(n))
      .sort();
  }

  async exists(repository: string): Promise<boolean> {
    try {
      return (await stat(this.dir(repository))).isDirectory();
    } catch {
      return false;
    }
  }

  async readMain(repository: string): Promise<Revision | null> {
    return this.resolveRef(repository, MAIN_REF);
  }

  private async resolveRef(repository: string, ref: string): Promise<Revision | null> {
    try {
      const out = await this.git(repository, ['rev-parse', '--verify', '--quiet', `${ref}^{commit}`]);
      return out.toString('utf8').trim();
    } catch (e) {
      if (e instanceof GitError) return null;
      throw e;
    }
  }

  private async buildTree(repository: string, parent: Revision | null, changes: FileChange[]): Promise<string> {
    const index = join(tmpdir(), `logiforge-index-${randomUUID()}`);
    const env = { GIT_INDEX_FILE: index };
    try {
      if (parent) await this.git(repository, ['read-tree', parent], undefined, env);
      else await this.git(repository, ['read-tree', '--empty'], undefined, env);
      const lines: string[] = [];
      for (const c of changes) {
        const sha = (await this.git(repository, ['hash-object', '-w', '--stdin'], c.content)).toString('utf8').trim();
        lines.push(`100644 ${sha}\t${c.path}\0`);
      }
      if (lines.length > 0) await this.git(repository, ['update-index', '-z', '--index-info'], lines.join(''), env);
      return (await this.git(repository, ['write-tree'], undefined, env)).toString('utf8').trim();
    } finally {
      await rm(index, { force: true });
    }
  }

  private async commit(repository: string, tree: string, parent: Revision | null, message: string): Promise<Revision> {
    const args = ['commit-tree', tree];
    if (parent) args.push('-p', parent);
    args.push('-F', '-');
    return (await this.git(repository, args, message, IDENTITY)).toString('utf8').trim();
  }

  async createInitialMain(repository: string, files: FileChange[], message: string): Promise<Revision> {
    const tree = await this.buildTree(repository, null, files);
    const commit = await this.commit(repository, tree, null, message);
    // 古い値を空にすると、main がまだないときだけ作れる
    await this.git(repository, ['update-ref', MAIN_REF, commit, '']);
    return commit;
  }

  private async lsTree(repository: string, revision: Revision, prefix: string): Promise<{ sha: string; path: string }[]> {
    if (!REVISION_PATTERN.test(revision)) return [];
    const args = ['ls-tree', '-r', '-z', '--full-tree', revision];
    if (prefix) args.push('--', prefix);
    let out: Buffer;
    try {
      out = await this.git(repository, args);
    } catch (e) {
      if (e instanceof GitError) return [];
      throw e;
    }
    const entries: { sha: string; path: string }[] = [];
    for (const line of out.toString('utf8').split('\0')) {
      if (!line) continue;
      const tab = line.indexOf('\t');
      const [, type, sha] = line.slice(0, tab).split(' ');
      if (type === 'blob' && sha) entries.push({ sha, path: line.slice(tab + 1) });
    }
    return entries;
  }

  private async catBlobs(repository: string, shas: string[]): Promise<Buffer[]> {
    if (shas.length === 0) return [];
    const out = await this.git(repository, ['cat-file', '--batch'], shas.map((s) => `${s}\n`).join(''));
    const blobs: Buffer[] = [];
    let pos = 0;
    for (let i = 0; i < shas.length; i++) {
      const nl = out.indexOf(0x0a, pos);
      const header = out.subarray(pos, nl).toString('utf8').split(' ');
      const size = Number(header[2]);
      blobs.push(out.subarray(nl + 1, nl + 1 + size));
      pos = nl + 1 + size + 1;
    }
    return blobs;
  }

  async readFiles(repository: string, revision: Revision, prefix: string): Promise<Map<string, Buffer>> {
    const entries = (await this.lsTree(repository, revision, prefix)).filter(
      (e) => prefix === '' || e.path === prefix || e.path.startsWith(prefix.endsWith('/') ? prefix : `${prefix}/`),
    );
    const blobs = await this.catBlobs(repository, entries.map((e) => e.sha));
    return new Map(entries.map((e, i) => [e.path, blobs[i]!]));
  }

  async readFile(repository: string, revision: Revision, path: string): Promise<Buffer | null> {
    const entry = (await this.lsTree(repository, revision, path)).find((e) => e.path === path);
    if (!entry) return null;
    return (await this.catBlobs(repository, [entry.sha]))[0]!;
  }

  async readRevision(repository: string, revision: Revision): Promise<RevisionInfo | null> {
    if (!REVISION_PATTERN.test(revision)) return null;
    try {
      const out = await this.git(repository, ['log', '-1', '--no-walk', `--format=${LOG_FORMAT}`, `${revision}^{commit}`]);
      return parseLog(out)[0] ?? null;
    } catch (e) {
      if (e instanceof GitError) return null;
      throw e;
    }
  }

  async listChanges(repository: string, from: Revision | null, to: Revision, paths: string[]): Promise<RevisionInfo[]> {
    if (paths.length === 0) return [];
    const range = from ? `${from}..${to}` : to;
    const out = await this.git(repository, ['log', '--first-parent', '--reverse', `--format=${LOG_FORMAT}`, range, '--', ...paths]);
    return parseLog(out);
  }

  async mergeBase(repository: string, a: Revision, b: Revision): Promise<Revision | null> {
    try {
      return (await this.git(repository, ['merge-base', a, b])).toString('utf8').trim() || null;
    } catch (e) {
      if (e instanceof GitError && e.exitCode === 1) return null;
      throw e;
    }
  }

  async listMainRevisions(repository: string): Promise<RevisionInfo[]> {
    if (!(await this.readMain(repository))) return [];
    const out = await this.git(repository, ['log', '--first-parent', '--reverse', `--format=${LOG_FORMAT}`, MAIN_REF]);
    return parseLog(out);
  }

  async isAncestor(repository: string, a: Revision, b: Revision): Promise<boolean> {
    if (!REVISION_PATTERN.test(a) || !REVISION_PATTERN.test(b)) return false;
    try {
      await this.git(repository, ['merge-base', '--is-ancestor', a, b]);
      return true;
    } catch (e) {
      if (e instanceof GitError && e.exitCode === 1) return false;
      if (e instanceof GitError) return false;
      throw e;
    }
  }

  async createRevision(repository: string, parent: Revision, changes: FileChange[], message: string, name: string): Promise<Revision> {
    const ref = namedRef(name);
    const tree = await this.buildTree(repository, parent, changes);
    const commit = await this.commit(repository, tree, parent, message);
    await this.git(repository, ['update-ref', ref, commit, '']);
    return commit;
  }

  async readNamed(repository: string, name: string): Promise<Revision | null> {
    return this.resolveRef(repository, namedRef(name));
  }

  async advanceMain(repository: string, expectedParent: Revision, revision: Revision): Promise<boolean> {
    try {
      await this.git(repository, ['update-ref', MAIN_REF, revision, expectedParent]);
      return true;
    } catch (e) {
      if (e instanceof GitError) return false;
      throw e;
    }
  }
}

function namedRef(name: string): string {
  if (!REF_NAME_PATTERN.test(name)) throw new Error(`リビジョンの名前が正しくない: ${name}`);
  return `${NAMED_REF_PREFIX}${name}`;
}

function parseLog(out: Buffer): RevisionInfo[] {
  return out
    .toString('utf8')
    .split('\x1e')
    .map((r) => r.replace(/^\n/, ''))
    .filter((r) => r.length > 0)
    .map((r) => {
      const [revision, treeId, parents, ct, ...rest] = r.split('\0');
      return {
        revision: revision!,
        treeId: treeId!,
        parents: parents ? parents.split(' ').filter(Boolean) : [],
        committedAt: new Date(Number(ct) * 1000),
        message: rest.join('\0'),
      };
    });
}
