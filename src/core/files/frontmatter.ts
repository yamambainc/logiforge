import { parse, stringify } from 'yaml';

/** YAML の front matter と Markdown の本文（design/files/layout.md の「ファイルの中身」） */
export interface FrontMatterDocument {
  data: Record<string, unknown> | null;
  body: string;
}

export function serializeDocument(doc: FrontMatterDocument): string {
  if (doc.data === null) return doc.body;
  const yaml = stringify(doc.data, { lineWidth: 0 });
  return `---\n${yaml}---\n${doc.body}`;
}

export class FrontMatterError extends Error {}

export function parseDocument(text: string): FrontMatterDocument {
  if (!text.startsWith('---\n')) return { data: null, body: text };
  const end = text.indexOf('\n---\n', 3);
  if (end < 0) throw new FrontMatterError('front matter が閉じていない');
  const data: unknown = parse(text.slice(4, end + 1));
  if (data === null || typeof data !== 'object' || Array.isArray(data)) {
    throw new FrontMatterError('front matter がオブジェクトでない');
  }
  return { data: data as Record<string, unknown>, body: text.slice(end + 5) };
}
