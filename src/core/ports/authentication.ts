/**
 * 認証のポート（design/backend/ports/authentication.md）。
 * どの操作を使えるかの判定（認可）は、このポートではなくバックエンドが行う。
 */
export type PrincipalKind = 'user' | 'client';

export interface Principal {
  id: string;
  kind: PrincipalKind;
}

export interface Authentication {
  /** トークンから主体を確かめる。正しくなければ null */
  authenticate(token: string): Promise<Principal | null>;
  /** 主体が repository を読めるか */
  canRead(principal: Principal, repository: string): Promise<boolean>;
}
