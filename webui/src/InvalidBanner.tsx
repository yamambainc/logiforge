import { useEffect, useState } from 'react';
import { getRepository, type Repository } from './api';

// 最新のコミットが valid でないことの知らせ（design/backend/invalid-main.md の「WebUI」）。木を読む API は、valid_commit の木を返すので、
// 画面の上に、そのことと、示している木のコミットを示す。
export function InvalidBanner({ repo }: { repo: string }) {
  const [r, setR] = useState<Repository | null>(null);
  useEffect(() => {
    let live = true;
    getRepository(repo).then((x) => live && setR(x)).catch(() => live && setR(null));
    return () => { live = false; };
  }, [repo]);
  if (!r?.invalid_since) return null;
  return (
    <div className="notice invalid" role="alert">
      <strong>最新のコミットは valid でない。</strong>
      <span>
        {r.valid_commit ? <>コミット {r.valid_commit.slice(0, 10)} の木を示している。</> : <>valid なコミットがないので、最新のコミットの木を示している。</>}
        （valid でなくなったのは {r.invalid_since.slice(0, 10)} から）
      </span>
    </div>
  );
}
