import type { Revision } from './tree-store.js';

/**
 * やりとりのポート（design/backend/ports/interaction.md）。
 * プロバイダは、ルールを判定しない。受け取ったものを置き、求められたら返すだけである。
 */
export type ApplyRequestState = 'open' | 'merged' | 'closed';
export type ReviewKind = 'approve' | 'request_changes' | 'comment';
export type HumanRequestKind = 'pvr' | 'decision';
export type MergeFailureReason = 'stale_target' | 'invariant_violation';

export interface ApplyRequestVersionRecord {
  version: number;
  revision: Revision;
}

export interface ReviewRecord {
  id: string;
  version: number;
  kind: ReviewKind;
  body: string | null;
  reviewerSessionId: string;
  promptRevision: string | null;
}

export interface MergeFailureRecord {
  version: number;
  reason: MergeFailureReason;
  detail: string;
}

export interface ApplyRequestRecord {
  id: string;
  description: string | null;
  state: ApplyRequestState;
  closedReason: string | null;
  closedBySessionId: string | null;
  versions: ApplyRequestVersionRecord[];
  reviews: ReviewRecord[];
  mergeFailures: MergeFailureRecord[];
}

export interface HumanRequestAnswerRecord {
  body: string;
  answeredByPrincipalId: string;
  answeredAt: Date;
}

export interface HumanRequestRecord {
  id: string;
  nodeId: string;
  kind: HumanRequestKind;
  body: string;
  createdBySessionId: string;
  createdAt: Date;
  answer: HumanRequestAnswerRecord | null;
}

export interface Page<T> {
  items: T[];
  /** 次のページの先頭の識別子より前の、最後の識別子。null なら最後のページ */
  nextAfter: string | null;
}

export interface ListOptions {
  /** この識別子より後のものを返す */
  after?: string;
  limit: number;
}

export interface Interaction {
  /** repository の、やりとりの側を用意する。すでに済んでいれば何もしない */
  prepareRepository(repository: string, providerRef: string): Promise<void>;

  /** 提案を作り、版 1 を足す */
  createApplyRequest(repository: string, input: { id: string; description: string | null; revision: Revision }): Promise<void>;
  /** 提案に新しい版を足し、その版の番号を返す */
  addVersion(repository: string, applyRequestId: string, revision: Revision): Promise<number>;
  getApplyRequest(repository: string, applyRequestId: string): Promise<ApplyRequestRecord | null>;
  listApplyRequests(repository: string, filter: { state?: ApplyRequestState }, options: ListOptions): Promise<Page<ApplyRequestRecord>>;
  recordMergeFailure(repository: string, applyRequestId: string, failure: MergeFailureRecord): Promise<void>;
  /** open の提案を close する。open でなければ false */
  closeApplyRequest(repository: string, applyRequestId: string, reason: string, sessionId: string): Promise<boolean>;
  /** 提案が反映されたことを記録し、提案を閉じる。すでに merged なら何もしない */
  markMerged(repository: string, applyRequestId: string): Promise<void>;

  addReview(repository: string, applyRequestId: string, review: ReviewRecord): Promise<void>;

  createHumanRequest(repository: string, request: Omit<HumanRequestRecord, 'answer'>): Promise<void>;
  /** 回答を記録する。すでに回答があれば false */
  answerHumanRequest(repository: string, humanRequestId: string, answer: HumanRequestAnswerRecord): Promise<boolean>;
  getHumanRequest(repository: string, humanRequestId: string): Promise<HumanRequestRecord | null>;
  listHumanRequests(
    repository: string,
    filter: { kind?: HumanRequestKind; answered?: boolean; nodeId?: string },
    options: ListOptions,
  ): Promise<Page<HumanRequestRecord>>;
}
