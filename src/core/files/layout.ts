/**
 * 木のファイル配置（design/files/layout.md）のパスと、ファイルの中身の読み書き。
 */
import { createHash } from 'node:crypto';
import { parseDocument, serializeDocument } from './frontmatter.js';

export const TREE_DIR = 'tree';
export const ARTIFACTS_DIR = 'artifacts/sha256';

export const HYPOTHESIS_FILE = 'hypothesis.md';
export const BACKGROUND_FILE = 'background.md';
export const STORY_FILE = 'story.md';
export const INACTIVATED_FILE = 'inactivated.md';
export const VERDICTS_DIR = 'verdicts';

export type ArtifactKind = 'file' | 'link';

export interface ArtifactRef {
  kind: ArtifactKind;
  name: string;
  sha256?: string;
  uri?: string;
  media_type?: string;
}

export type Citation =
  | { target_kind: 'artifact'; sha256: string }
  | { target_kind: 'subtree'; node_id: string; revision: string };

export interface HypothesisContent {
  id: string;
  statement: string;
}

export interface StoryContent {
  id: string;
  title: string;
  summary: string;
}

export interface VerdictContent {
  id: string;
  value: boolean;
  supersedes: string | null;
  artifacts: ArtifactRef[];
  citations: Citation[];
  report: string;
}

export interface AnswerContent {
  human_request_id: string;
  answered_by: string;
  answered_at: string;
  attachments: ArtifactRef[];
  body: string;
}

export function sha256Hex(content: Buffer): string {
  return createHash('sha256').update(content).digest('hex');
}

export function artifactPath(sha256: string): string {
  return `${ARTIFACTS_DIR}/${sha256.slice(0, 2)}/${sha256.slice(2)}`;
}

/** artifacts/sha256/ のパスから sha256 を求める。形が違えば null */
export function sha256FromArtifactPath(path: string): string | null {
  const m = /^artifacts\/sha256\/([0-9a-f]{2})\/([0-9a-f]{62})$/.exec(path);
  return m ? `${m[1]}${m[2]}` : null;
}

export function verdictPath(hypothesisDir: string, verdictId: string): string {
  return `${hypothesisDir}/${VERDICTS_DIR}/${verdictId}.md`;
}

export function serializeHypothesis(c: HypothesisContent): Buffer {
  return Buffer.from(serializeDocument({ data: { id: c.id, statement: c.statement }, body: '' }));
}

export function serializeStory(c: StoryContent): Buffer {
  return Buffer.from(serializeDocument({ data: { id: c.id, title: c.title, summary: c.summary }, body: '' }));
}

export function serializeVerdict(c: VerdictContent): Buffer {
  const data: Record<string, unknown> = { id: c.id, value: c.value };
  if (c.supersedes !== null) data.supersedes = c.supersedes;
  data.artifacts = c.artifacts;
  data.citations = c.citations;
  return Buffer.from(serializeDocument({ data, body: c.report }));
}

export function serializeAnswer(c: AnswerContent): Buffer {
  return Buffer.from(
    serializeDocument({
      data: {
        human_request_id: c.human_request_id,
        answered_by: c.answered_by,
        answered_at: c.answered_at,
        attachments: c.attachments,
      },
      body: c.body,
    }),
  );
}

export class FileFormatError extends Error {}

function str(data: Record<string, unknown>, key: string, path: string): string {
  const v = data[key];
  if (typeof v !== 'string') throw new FileFormatError(`${path}: ${key} が文字列でない`);
  return v;
}

function requireData(text: string, path: string): Record<string, unknown> {
  const doc = parseDocument(text);
  if (doc.data === null) throw new FileFormatError(`${path}: front matter がない`);
  return doc.data;
}

function onlyKeys(data: Record<string, unknown>, keys: string[], path: string): void {
  for (const k of Object.keys(data)) {
    if (!keys.includes(k)) throw new FileFormatError(`${path}: 決まっていない項目 ${k} がある`);
  }
}

export function parseHypothesis(text: string, path: string): HypothesisContent {
  const data = requireData(text, path);
  onlyKeys(data, ['id', 'statement'], path);
  return { id: str(data, 'id', path), statement: str(data, 'statement', path) };
}

export function parseStory(text: string, path: string): StoryContent {
  const data = requireData(text, path);
  onlyKeys(data, ['id', 'title', 'summary'], path);
  return { id: str(data, 'id', path), title: str(data, 'title', path), summary: str(data, 'summary', path) };
}

function parseArtifacts(v: unknown, path: string): ArtifactRef[] {
  if (v === undefined) return [];
  if (!Array.isArray(v)) throw new FileFormatError(`${path}: artifacts が配列でない`);
  return v.map((a: unknown) => {
    if (a === null || typeof a !== 'object') throw new FileFormatError(`${path}: artifact の形が正しくない`);
    const o = a as Record<string, unknown>;
    if (o.kind !== 'file' && o.kind !== 'link') throw new FileFormatError(`${path}: artifact の kind が正しくない`);
    if (typeof o.name !== 'string') throw new FileFormatError(`${path}: artifact の name がない`);
    const ref: ArtifactRef = { kind: o.kind, name: o.name };
    for (const k of ['sha256', 'uri', 'media_type'] as const) {
      if (o[k] !== undefined) {
        if (typeof o[k] !== 'string') throw new FileFormatError(`${path}: artifact の ${k} が文字列でない`);
        ref[k] = o[k];
      }
    }
    if (ref.kind === 'file' && ref.sha256 === undefined) throw new FileFormatError(`${path}: file の artifact に sha256 がない`);
    if (ref.kind === 'link' && ref.uri === undefined) throw new FileFormatError(`${path}: link の artifact に uri がない`);
    return ref;
  });
}

function parseCitations(v: unknown, path: string): Citation[] {
  if (v === undefined) return [];
  if (!Array.isArray(v)) throw new FileFormatError(`${path}: citations が配列でない`);
  return v.map((c: unknown) => {
    const o = (c ?? {}) as Record<string, unknown>;
    if (o.target_kind === 'artifact' && typeof o.sha256 === 'string') return { target_kind: 'artifact', sha256: o.sha256 };
    if (o.target_kind === 'subtree' && typeof o.node_id === 'string' && typeof o.revision === 'string') {
      return { target_kind: 'subtree', node_id: o.node_id, revision: o.revision };
    }
    throw new FileFormatError(`${path}: 引用の形が正しくない`);
  });
}

export function parseVerdict(text: string, path: string): VerdictContent {
  const doc = parseDocument(text);
  if (doc.data === null) throw new FileFormatError(`${path}: front matter がない`);
  const data = doc.data;
  onlyKeys(data, ['id', 'value', 'supersedes', 'artifacts', 'citations'], path);
  if (typeof data.value !== 'boolean') throw new FileFormatError(`${path}: value が真偽値でない`);
  const supersedes = data.supersedes === undefined ? null : str(data, 'supersedes', path);
  return {
    id: str(data, 'id', path),
    value: data.value,
    supersedes,
    artifacts: parseArtifacts(data.artifacts, path),
    citations: parseCitations(data.citations, path),
    report: doc.body,
  };
}

export function parseAnswer(text: string, path: string): AnswerContent {
  const doc = parseDocument(text);
  if (doc.data === null) throw new FileFormatError(`${path}: front matter がない`);
  const d = doc.data;
  return {
    human_request_id: str(d, 'human_request_id', path),
    answered_by: str(d, 'answered_by', path),
    answered_at: str(d, 'answered_at', path),
    attachments: parseArtifacts(d.attachments, path),
    body: doc.body,
  };
}
