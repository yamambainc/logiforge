/**
 * 木の読み書きのポート（design/backend/ports/tree-read-write.md）。
 * リビジョンは、ポートの外からは不透明な文字列として扱う。
 */
export type Revision = string;

/** リビジョンの情報。署名の検証と履歴に使う */
export interface RevisionInfo {
  revision: Revision;
  /** リビジョンが指す木の識別子。署名の対象に含める */
  treeId: string;
  parents: Revision[];
  message: string;
  committedAt: Date;
}

/** 新しいリビジョンに足すファイル。すでにあるパスなら中身を置き換える */
export interface FileChange {
  path: string;
  content: Buffer;
}

export interface TreeStore {
  /** repository の git リポジトリを、空の状態で作る。すでにあれば何もしない */
  prepare(repository: string): Promise<void>;
  /** 用意済みの repository の名前を列挙する */
  list(): Promise<string[]>;
  /** 用意済みかどうか */
  exists(repository: string): Promise<boolean>;
  /** 今の main のリビジョン。main がなければ null */
  readMain(repository: string): Promise<Revision | null>;
  /** 親を持たないリビジョンを作り、main をそこに置く。main がすでにあれば失敗する */
  createInitialMain(repository: string, files: FileChange[], message: string): Promise<Revision>;
  /** あるリビジョンの、prefix の下にあるファイルを読む */
  readFiles(repository: string, revision: Revision, prefix: string): Promise<Map<string, Buffer>>;
  /** あるリビジョンの 1 つのファイルを読む。なければ null */
  readFile(repository: string, revision: Revision, path: string): Promise<Buffer | null>;
  /** リビジョンの情報を読む。なければ null */
  readRevision(repository: string, revision: Revision): Promise<RevisionInfo | null>;
  /** from（含まない）から to までの first-parent の並びで、paths のどれかを変えたリビジョンを古い順に返す */
  listChanges(repository: string, from: Revision | null, to: Revision, paths: string[]): Promise<RevisionInfo[]>;
  /** 2 つのリビジョンの共通の祖先。なければ null */
  mergeBase(repository: string, a: Revision, b: Revision): Promise<Revision | null>;
  /** main が通ってきたリビジョンを、古い順に読む */
  listMainRevisions(repository: string): Promise<RevisionInfo[]>;
  /** a が b の祖先（または同じ）かどうか */
  isAncestor(repository: string, a: Revision, b: Revision): Promise<boolean>;
  /** 親のリビジョンにファイルを足した新しいリビジョンを作り、name に置く。main は動かさない */
  createRevision(repository: string, parent: Revision, changes: FileChange[], message: string, name: string): Promise<Revision>;
  /** 名前を付けて置いたリビジョン。なければ null */
  readNamed(repository: string, name: string): Promise<Revision | null>;
  /** main を expectedParent から revision へ進める。main が expectedParent でなければ false */
  advanceMain(repository: string, expectedParent: Revision, revision: Revision): Promise<boolean>;
}
