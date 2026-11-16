// ファイルの保存と、回答の添付の大きさ（design/webui/security.md の「ファイルの開き方」、design/api/README.md の「アップロードの大きさ」）。

/** ファイルは、その場で開かず、保存させるだけにする */
export function saveBlob(b: Blob, name: string) {
  const url = URL.createObjectURL(new Blob([b], { type: 'application/octet-stream' }));
  const link = document.createElement('a');
  link.href = url; link.download = name;
  document.body.appendChild(link); link.click(); link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export const MAX_FILE = 10 * 1024 * 1024; // 1 つのファイルの中身
export const MAX_REQUEST = 32 * 1024 * 1024; // 1 回の要求の本文

/** base64 にしたときの長さ */
export const base64Len = (n: number) => 4 * Math.ceil(n / 3);

/** 回答の要求の本文の大きさの見積もり（本文と、添付の base64 と、名前などの余り） */
export function requestSize(body: string, files: { name: string; size: number }[], links: { name: string; uri: string }[]): number {
  const enc = new TextEncoder();
  let n = enc.encode(body).length * 2 + 64; // JSON のエスケープで増える分を、多めに見る
  for (const f of files) n += base64Len(f.size) + enc.encode(f.name).length * 2 + 96;
  for (const l of links) n += enc.encode(l.name + l.uri).length * 2 + 64;
  return n;
}

/** 添付を確かめる。だめなら、理由を返す */
export function checkAttachments(body: string, files: { name: string; size: number }[], links: { name: string; uri: string }[]): string | null {
  const big = files.find((f) => f.size > MAX_FILE);
  if (big) return `「${big.name}」は 10 MiB を超えています。大きなファイルは、置き場に置いてリンクで添えてください。`;
  if (requestSize(body, files, links) > MAX_REQUEST) return 'ファイルの合計が大きすぎます（1 回の回答は、base64 にして 32 MiB まで）。いくつかはリンクで添えてください。';
  const bad = links.find((l) => !/^https?:\/\//.test(l.uri));
  if (bad) return `リンク「${bad.name}」は http か https の URL にしてください。`;
  return null;
}

export function toBase64(buf: ArrayBuffer): string {
  const b = new Uint8Array(buf);
  let s = '';
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000));
  return btoa(s);
}

// --- 回答の書きかけ（design/webui/auth.md の「トークンの持ち方」） ---
// 本文と、添付の名前とリンクを sessionStorage に置く。トークンではないので置いてよい。ファイルの中身は置かない。

export interface Draft { body: string; fileNames: string[]; links: { name: string; uri: string }[] }
const draftKey = (repo: string, id: string) => `lf-draft:${repo}:${id}`;

export function saveDraft(repo: string, id: string, d: Draft) {
  if (!d.body && d.fileNames.length === 0 && d.links.length === 0) sessionStorage.removeItem(draftKey(repo, id));
  else sessionStorage.setItem(draftKey(repo, id), JSON.stringify(d));
}
export function loadDraft(repo: string, id: string): Draft | null {
  try { const v = sessionStorage.getItem(draftKey(repo, id)); return v ? (JSON.parse(v) as Draft) : null; } catch { return null; }
}
export const clearDraft = (repo: string, id: string) => sessionStorage.removeItem(draftKey(repo, id));
