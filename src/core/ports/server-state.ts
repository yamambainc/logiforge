/**
 * サーバの状態のポート（design/backend/ports/server-state.md）。
 * サーバの状態は、すべて作り直せるキャッシュである。
 */
export interface RepositoryRegistration {
  name: string;
  providerRef: string;
}

export interface StoredResponse {
  status: number;
  body: unknown;
}

export interface ReconcileState {
  lastReconciledMain: string;
  lastReconciledAt: Date;
}

export interface ServerState {
  upsertRepository(registration: RepositoryRegistration): Promise<void>;
  getRepository(name: string): Promise<RepositoryRegistration | null>;
  /** 名前の順に返す */
  listRepositories(options: { after?: string; limit: number }): Promise<RepositoryRegistration[]>;

  /** createdAt が since より新しい、同じ主体の同じキーの応答 */
  getIdempotentResponse(key: string, principalId: string, since: Date): Promise<StoredResponse | null>;
  putIdempotentResponse(key: string, principalId: string, response: StoredResponse, createdAt: Date): Promise<void>;

  getReconcileState(repository: string): Promise<ReconcileState | null>;
  putReconcileState(repository: string, state: ReconcileState): Promise<void>;
}
