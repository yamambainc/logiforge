import { forgejoOrigin, gitforgeName, retryTarget, signIn, type SignedOutReason } from '../auth';

const messages = (g: string): Record<SignedOutReason, string> => ({
  'signed-out': 'サインアウトしました。',
  denied: `${g} で、サインインが許可されませんでした。`,
  'state-mismatch': 'サインインの確かめに失敗しました。もう一度サインインしてください。',
  'exchange-failed': 'サインインのコードを交換できませんでした。もう一度サインインしてください。',
  unavailable: `${g} につながりませんでした。しばらくしてから、もう一度サインインしてください。`,
  'not-member': g === 'GitHub'
    ? 'このアカウントでは、Logiforge を使えません。GitHub で、Logiforge の GitHub App と、登録したリポジトリへの権限を確かめてください。'
    : 'Logiforge の organization に入っていません。管理者に、organization に入れてもらってください。',
  'not-configured': 'このサーバでは、WebUI のサインインがまだ用意されていません。セットアップを終えるか、サーバを動かす人に知らせてください。',
});

// サインインの画面（design/webui/auth.md）。自動で authorize に行かない場面でだけ出す。
export function SignIn({ reason }: { reason: SignedOutReason }) {
  const forgejo = forgejoOrigin();
  const g = gitforgeName();
  return (
    <div className="signin">
      <div className="signin-box">
        <div className="brand big">Logiforge</div>
        <p className="signin-msg">{messages(g)[reason]}</p>
        {reason !== 'not-configured' && (
          <button className="btn primary" onClick={() => void signIn(retryTarget())}>{g} でサインイン</button>
        )}
        {reason === 'signed-out' && forgejo && (
          <p className="dim small">
            {g} にログインしたままだと、押す操作なしにサインインし直します。外れるには、<a href={forgejo} rel="noreferrer">{g}</a> からもログアウトしてください。
          </p>
        )}
      </div>
    </div>
  );
}
