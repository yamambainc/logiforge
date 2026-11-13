import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach } from 'vitest';

/** テストごとの一時ディレクトリ。テストが終わると消す */
export function useTempDir(): () => Promise<string> {
  const dirs: string[] = [];
  afterEach(async () => {
    await Promise.all(dirs.splice(0).map((d) => rm(d, { recursive: true, force: true })));
  });
  return async () => {
    const d = await mkdtemp(join(tmpdir(), 'logiforge-test-'));
    dirs.push(d);
    return d;
  };
}
