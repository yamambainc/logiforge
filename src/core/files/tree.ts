/**
 * tree/ のファイルから、木を組み立てる（design/files/layout.md）。
 * 形の誤りは例外にせず errors に集める。差分の検査（design/backend/checks.md）が使う。
 */
import { HYPOTHESIS_ID_PATTERN, STORY_ID_PATTERN, VERDICT_ID_PATTERN } from '../ids.js';
import {
  BACKGROUND_FILE,
  FileFormatError,
  HYPOTHESIS_FILE,
  INACTIVATED_FILE,
  STORY_FILE,
  TREE_DIR,
  VERDICTS_DIR,
  parseHypothesis,
  parseStory,
  parseVerdict,
  type VerdictContent,
} from './layout.js';
import { FrontMatterError } from './frontmatter.js';

export interface TreeVerdict extends VerdictContent {
  path: string;
  hypothesis: TreeHypothesis;
}

export interface TreeHypothesis {
  kind: 'hypothesis';
  id: string;
  dir: string;
  /** 根から数えた段。根が 1 */
  depth: number;
  parent: TreeStory | null;
  statement: string;
  background: string;
  verdicts: TreeVerdict[];
  stories: TreeStory[];
}

export interface TreeStory {
  kind: 'story';
  id: string;
  dir: string;
  parent: TreeHypothesis;
  title: string;
  summary: string;
  /** inactivated.md の本文。inactive にしていなければ null */
  inactivatedReason: string | null;
  hypotheses: TreeHypothesis[];
}

export type TreeNode = TreeHypothesis | TreeStory;

export interface ParsedTree {
  /** tree/ の直下の仮説。正しい木なら 1 つだけ */
  roots: TreeHypothesis[];
  hypotheses: Map<string, TreeHypothesis>;
  stories: Map<string, TreeStory>;
  verdicts: Map<string, TreeVerdict>;
  errors: string[];
}

interface DirEntry {
  files: Map<string, Buffer>;
  dirs: Map<string, DirEntry>;
}

function newDir(): DirEntry {
  return { files: new Map(), dirs: new Map() };
}

function toDirTree(files: Map<string, Buffer>): DirEntry {
  const top = newDir();
  for (const [path, content] of files) {
    const parts = path.split('/');
    let cur = top;
    for (const p of parts.slice(0, -1)) {
      let next = cur.dirs.get(p);
      if (!next) {
        next = newDir();
        cur.dirs.set(p, next);
      }
      cur = next;
    }
    cur.files.set(parts[parts.length - 1]!, content);
  }
  return top;
}

function sortedKeys<T>(m: Map<string, T>): string[] {
  return [...m.keys()].sort();
}

export function buildTree(files: Map<string, Buffer>): ParsedTree {
  const result: ParsedTree = { roots: [], hypotheses: new Map(), stories: new Map(), verdicts: new Map(), errors: [] };
  const top = toDirTree(files);
  const treeDir = top.dirs.get(TREE_DIR);
  for (const name of top.files.keys()) result.errors.push(`${name}: 決まっていないファイル`);
  for (const name of top.dirs.keys()) {
    if (name !== TREE_DIR && name !== 'artifacts') result.errors.push(`${name}/: 決まっていないディレクトリ`);
  }
  if (!treeDir) return result;
  for (const name of treeDir.files.keys()) result.errors.push(`${TREE_DIR}/${name}: 決まっていないファイル`);

  const guard = <T>(path: string, f: () => T): T | null => {
    try {
      return f();
    } catch (e) {
      if (e instanceof FileFormatError || e instanceof FrontMatterError) {
        result.errors.push(e.message.startsWith(path) ? e.message : `${path}: ${e.message}`);
        return null;
      }
      throw e;
    }
  };

  const readHypothesis = (name: string, entry: DirEntry, dir: string, parent: TreeStory | null): TreeHypothesis => {
    const h: TreeHypothesis = {
      kind: 'hypothesis',
      id: name,
      dir,
      depth: parent ? parent.parent.depth + 1 : 1,
      parent,
      statement: '',
      background: '',
      verdicts: [],
      stories: [],
    };
    result.hypotheses.set(name, h);
    const main = entry.files.get(HYPOTHESIS_FILE);
    if (!main) {
      result.errors.push(`${dir}: ${HYPOTHESIS_FILE} がない`);
    } else {
      const c = guard(`${dir}/${HYPOTHESIS_FILE}`, () => parseHypothesis(main.toString('utf8'), `${dir}/${HYPOTHESIS_FILE}`));
      if (c) {
        if (c.id !== name) result.errors.push(`${dir}/${HYPOTHESIS_FILE}: id がディレクトリ名と違う`);
        h.statement = c.statement;
      }
    }
    const bg = entry.files.get(BACKGROUND_FILE);
    if (bg) h.background = bg.toString('utf8');
    for (const f of entry.files.keys()) {
      if (f !== HYPOTHESIS_FILE && f !== BACKGROUND_FILE) result.errors.push(`${dir}/${f}: 決まっていないファイル`);
    }
    for (const d of sortedKeys(entry.dirs)) {
      const sub = entry.dirs.get(d)!;
      const subDir = `${dir}/${d}`;
      if (d === VERDICTS_DIR) {
        for (const d2 of sub.dirs.keys()) result.errors.push(`${subDir}/${d2}/: 決まっていないディレクトリ`);
        for (const f of sortedKeys(sub.files)) {
          const path = `${subDir}/${f}`;
          const m = /^(v-.*)\.md$/.exec(f);
          if (!m || !VERDICT_ID_PATTERN.test(m[1]!)) {
            result.errors.push(`${path}: 決まっていないファイル`);
            continue;
          }
          const v = guard(path, () => parseVerdict(sub.files.get(f)!.toString('utf8'), path));
          if (!v) continue;
          if (v.id !== m[1]) result.errors.push(`${path}: id がファイル名と違う`);
          if (result.verdicts.has(v.id)) result.errors.push(`${path}: 検証結果の識別子が重なっている`);
          const tv: TreeVerdict = { ...v, path, hypothesis: h };
          h.verdicts.push(tv);
          result.verdicts.set(v.id, tv);
        }
      } else if (STORY_ID_PATTERN.test(d)) {
        h.stories.push(readStory(d, sub, subDir, h));
      } else {
        result.errors.push(`${subDir}/: 仮説の子にストーリーでないディレクトリがある`);
      }
    }
    return h;
  };

  const readStory = (name: string, entry: DirEntry, dir: string, parent: TreeHypothesis): TreeStory => {
    const s: TreeStory = { kind: 'story', id: name, dir, parent, title: '', summary: '', inactivatedReason: null, hypotheses: [] };
    if (result.stories.has(name) || result.hypotheses.has(name)) result.errors.push(`${dir}: 識別子が重なっている`);
    result.stories.set(name, s);
    const main = entry.files.get(STORY_FILE);
    if (!main) {
      result.errors.push(`${dir}: ${STORY_FILE} がない`);
    } else {
      const c = guard(`${dir}/${STORY_FILE}`, () => parseStory(main.toString('utf8'), `${dir}/${STORY_FILE}`));
      if (c) {
        if (c.id !== name) result.errors.push(`${dir}/${STORY_FILE}: id がディレクトリ名と違う`);
        s.title = c.title;
        s.summary = c.summary;
      }
    }
    const inact = entry.files.get(INACTIVATED_FILE);
    if (inact) s.inactivatedReason = inact.toString('utf8');
    for (const f of entry.files.keys()) {
      if (f !== STORY_FILE && f !== INACTIVATED_FILE) result.errors.push(`${dir}/${f}: 決まっていないファイル`);
    }
    for (const d of sortedKeys(entry.dirs)) {
      if (HYPOTHESIS_ID_PATTERN.test(d)) {
        if (result.hypotheses.has(d)) result.errors.push(`${dir}/${d}: 識別子が重なっている`);
        s.hypotheses.push(readHypothesis(d, entry.dirs.get(d)!, `${dir}/${d}`, s));
      } else {
        result.errors.push(`${dir}/${d}/: ストーリーの子に仮説でないディレクトリがある`);
      }
    }
    return s;
  };

  for (const d of sortedKeys(treeDir.dirs)) {
    if (HYPOTHESIS_ID_PATTERN.test(d)) {
      result.roots.push(readHypothesis(d, treeDir.dirs.get(d)!, `${TREE_DIR}/${d}`, null));
    } else {
      result.errors.push(`${TREE_DIR}/${d}/: 根が仮説のディレクトリでない`);
    }
  }
  return result;
}

/** 自分か祖先のストーリーに inactivated.md があるか（design/files/layout.md の「保存しないもの」） */
export function isInactive(node: TreeNode): boolean {
  let cur: TreeNode | null = node;
  while (cur) {
    if (cur.kind === 'story') {
      if (cur.inactivatedReason !== null) return true;
      cur = cur.parent;
    } else {
      cur = cur.parent;
    }
  }
  return false;
}

/** 有効な検証結果。ほかの検証結果の supersedes に書かれていないもの */
export function validVerdicts(h: TreeHypothesis): TreeVerdict[] {
  const superseded = new Set(h.verdicts.map((v) => v.supersedes).filter((x): x is string => x !== null));
  return h.verdicts.filter((v) => !superseded.has(v.id));
}

export function findNode(tree: ParsedTree, id: string): TreeNode | null {
  return tree.hypotheses.get(id) ?? tree.stories.get(id) ?? null;
}
