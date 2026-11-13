import { randomBytes } from 'node:crypto';

/** Crockford の base32（小文字）。design/files/layout.md の「識別子」 */
const CROCKFORD = '0123456789abcdefghjkmnpqrstvwxyz';

export type NodeKind = 'h' | 's' | 'v';

export const NODE_ID_PATTERN = /^[hsv]-[0-9a-hjkmnp-tv-z]{10}$/;
export const HYPOTHESIS_ID_PATTERN = /^h-[0-9a-hjkmnp-tv-z]{10}$/;
export const STORY_ID_PATTERN = /^s-[0-9a-hjkmnp-tv-z]{10}$/;
export const VERDICT_ID_PATTERN = /^v-[0-9a-hjkmnp-tv-z]{10}$/;
export const SHA256_PATTERN = /^[0-9a-f]{64}$/;
export const REPOSITORY_NAME_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;

function base32(bytes: Buffer, length: number): string {
  let out = '';
  let bits = 0;
  let value = 0;
  for (const b of bytes) {
    value = (value << 8) | b;
    bits += 8;
    while (bits >= 5 && out.length < length) {
      out += CROCKFORD[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  return out;
}

/** 種類の接頭辞と、乱数で決めた 10 文字をつなぐ */
export function newNodeId(kind: NodeKind): string {
  return `${kind}-${base32(randomBytes(7), 10)}`;
}

export function nodeKindOf(id: string): NodeKind | null {
  if (HYPOTHESIS_ID_PATTERN.test(id)) return 'h';
  if (STORY_ID_PATTERN.test(id)) return 's';
  if (VERDICT_ID_PATTERN.test(id)) return 'v';
  return null;
}
