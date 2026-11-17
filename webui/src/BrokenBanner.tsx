import { useEffect, useState } from 'react';
import { getRepository, type Repository } from './api';

// 壊れた repository の知らせ（design/backend/broken.md の「バックエンド」）。木を読む API は、最後に正しかった時点の木を返すので、
// 画面の上に、そのことと、壊れた最初のコミットを示す。
export function BrokenBanner({ repo }: { repo: string }) {
  const [broken, setBroken] = useState<Repository['broken']>(null);
  useEffect(() => {
    let live = true;
    getRepository(repo).then((r) => live && setBroken(r.broken ?? null)).catch(() => live && setBroken(null));
    return () => { live = false; };
  }, [repo]);
  if (!broken) return null;
  const why = broken.reason === 'main_rewritten' ? 'main が書き換えられました' : '反映によらない変更が main に入りました';
  return (
    <div className="notice broken" role="alert">
      <strong>この repository は壊れています。</strong>
      <span>
        {why}（{broken.first_commit.slice(0, 10)}）。表示しているのは、最後に正しかった時点 {broken.last_good_commit.slice(0, 10)} の木です。
        直すための apply request（戻す、または認める）が反映されるまで、ほかの apply request は反映されません。
      </span>
    </div>
  );
}
