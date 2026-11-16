import { useEffect, useState } from 'react';

const MOBILE = '(max-width: 767px)';

/** 幅 767 px 以下ならモバイル（design/webui/mobile.md の「レイアウトの切り替え」） */
export function useMobile() {
  const [m, setM] = useState(() => matchMedia(MOBILE).matches);
  useEffect(() => {
    const mq = matchMedia(MOBILE);
    const on = () => setM(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  return m;
}

export const isMobileNow = () => matchMedia(MOBILE).matches;
