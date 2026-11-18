// 起動したときに、セットアップの画面を出すか、ふだんの画面を出すかを決める（design/webui/signin-flow.md の「セットアップが終わっていないとき」）。
import { CALLBACK_PATH, clearSetupStorage, getSetup, type SetupState } from './setup';

export type Boot = { kind: 'setup'; state: SetupState } | { kind: 'app' };

/** getSetup を呼び、終わっていなければ authorize に行かない。サインインから戻ったときだけ、交換する（戻り先は /setup）。
 * getSetup が失敗したら（つながらない、古い BE）、ふだんどおりに動く。 */
export async function boot(start: () => Promise<void>): Promise<Boot> {
  let s: SetupState | null = null;
  try { s = await getSetup(); } catch { s = null; }
  if (s && !s.done) {
    if (location.pathname === '/oauth/callback') void start();
    // どの URL を開いても /setup を出す
    else if (location.pathname !== '/setup' && location.pathname !== CALLBACK_PATH) history.replaceState(null, '', '/setup');
    return { kind: 'setup', state: s };
  }
  if (s?.done) clearSetupStorage();
  void start();
  return { kind: 'app' };
}
