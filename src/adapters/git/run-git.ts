import { spawn } from 'node:child_process';

export class GitError extends Error {
  constructor(
    message: string,
    readonly exitCode: number | null,
    readonly stderr: string,
  ) {
    super(message);
  }
}

export interface RunGitOptions {
  cwd: string;
  input?: Buffer | string;
  env?: Record<string, string>;
}

/** git CLI を呼び、標準出力を返す。終了コードが 0 でなければ GitError */
export function runGit(args: string[], options: RunGitOptions): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const child = spawn('git', args, {
      cwd: options.cwd,
      env: { ...process.env, ...options.env, GIT_TERMINAL_PROMPT: '0', LC_ALL: 'C' },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    const out: Buffer[] = [];
    const err: Buffer[] = [];
    child.stdout.on('data', (d: Buffer) => out.push(d));
    child.stderr.on('data', (d: Buffer) => err.push(d));
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) resolve(Buffer.concat(out));
      else {
        const stderr = Buffer.concat(err).toString('utf8');
        reject(new GitError(`git ${args[0]} が失敗した（${code}）: ${stderr.trim()}`, code, stderr));
      }
    });
    child.stdin.on('error', () => undefined);
    child.stdin.end(options.input ?? '');
  });
}
