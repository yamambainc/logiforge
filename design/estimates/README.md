# 費用と規模の見積もり

複数人で使う構成（GitForge と静的ファイルストレージ）を、どれくらいの費用と規模で動かせるかを確かめるときに読む。
想定は、利用者とエージェントが数人から十数人、repository が数個から数十個である。
スナップショットツリーを作る時間、読む時間、大きさは、[スナップショットの時間と大きさ](snapshot.md) に書く。

## 想定する規模

- repository は 30 個、1 つの木のノードは 1,000 個ほどまでとする。
- 書き込み（apply request の作成と push、レビュー、close、起票、回答、反映）は、全体で 1 時間に 100 件ほどまでとする。
  - そのうち反映（`main` への push）は、1 日に数十件から 100 件ほどとする。
- 木を読む要求は、全体で 1 時間に 1 万件ほどまでとする。内訳は次のとおりである。
  - 外の仕組みが、作業の一覧と作業の終わりの判定を、動いている repository 10 個について、30 秒ごとに読む：1 時間 2,400 件
  - エージェント 10 人が、作業の前後に木やノードを読む：1 人 1 時間 400 件で 4,000 件
  - 利用者 10 人が、WebUI で木とノードを見る：1 人 1 時間 300 件で 3,000 件

## GitForge の API のレート制限

- バックエンドは、やりとりと認証を、要求ごとに GitForge から読む。結果を保持しない（[状態を持たない](../backend/README.md)）。
- そのため、多くの人とエージェントが頻繁に読むと、GitForge の API のレート制限に当たることがある。
- バックエンドは、レート制限を避ける仕組みを持たない。
- GitForge ごとの回数の見積もりは、[アダプタごとの見積もり](#アダプタごとの見積もり) に書く。

## worker

- worker は、小さな常駐のコンテナ 1 つで足りる。
  - 仕事は、変わった repository の fetch と、スナップショット、ブランチの履歴、`latest` を書くことである。git には書かない。
- clone は、artifact の中身を含むので、repository ごとに数十 MB から数百 MB である。
  - repository 30 個で、永続するストレージは、数 GB から 10 GB ほどである。
- worker は、10 秒に 1 回、repository の一覧を読み直す（[見回り](../backend/worker/patrol.md)）。1 日に 8,640 回である。
  - GitHub の構成では、設定の DB から読む。Forgejo の構成では、organization の一覧を Forgejo の API で読み、ディレクトリの repository を設定の DB から読む。
- 見回りの `git ls-remote` は、repository 30 個 × 1 秒ごとで、1 秒に 30 回、1 日に約 260 万回である。1 回数 KB（ブランチの数による）である。
  - GitForge の API の上限には数えないことが多いが、git の操作の、乱用を防ぐ制限には数える。
  - `ls-remote` 1 回ごとに、GitForge は認証と ref の一覧を返す処理をする。
  - GitHub を使う構成では、間隔を長めにする（[見回りの間隔](../adapters/github.md#見回りの間隔)）。
  - 間隔を 1 秒にするのは、push と書き込みが読む側に出るまでの遅れを短くするためである（[見回り](../backend/worker/patrol.md)）。GitForge の制限に当たるなら、間隔を延ばす。延ばした分だけ、書き込みの API の応答が遅くなる。
- バックエンドは、repository ごとに、作業用の git の複製を持つ（[作業用の git の複製](../backend/apply-request/create.md#作業用の-git-の複製)）。
  - clone は、プロセスごと、repository ごとに 1 度だけである。そのあとの要求では fetch だけをするので、書き込みのたびの clone はない。

## 読む量と費用

- バックエンドはキャッシュを持たないので、木を読む要求ごとに、`latest`（数 KB）と、`main` のスナップショットツリー（圧縮して数百 KB）を置き場から読む。
  - 木を読む要求が 1 時間 1 万件、1 回 1 MB なら、1 時間 10 GB、1 か月で 7 TB ほどである。
- 置き場は、バックエンドから転送の費用なしで読める場所に置く（[スナップショットの置き場のアダプタ](../adapters/snapshot-store.md)）。
  - そうすると、かかるのは、読む回数の費用だけである。1 か月 700 万回で、数ドルほどである。
  - 転送に費用がかかる場所に置くと、7 TB で数百ドルになる。
- CDN は任意である。CDN からバックエンドへの転送は、多くの CDN でインターネットへの転送として費用がかかるので、費用を下げる手段にはならない。読むのが遅いことが問題になったときに置く。
- スナップショットツリーは、部分木ごとに分けずに、1 つのファイルで置く（[スナップショット](../backend/snapshot/README.md)）。

## アダプタごとの見積もり

### GitHub アダプタ

GitHub の REST の上限は、使うトークンで 2 つに分かれる（[GitHub App とトークン](../adapters/github-app.md#アカウントとトークン)）。

- 主体ごとの上限：バックエンドは、要求を送った主体のトークンで GitHub を読み書きする。上限は、主体のアカウントごとに 1 時間 5,000 回である。同じアカウントのほかのトークン（`gh auth login` のものなど）と合わせて数える。
- installation の上限：worker は、バックエンドが作った App の installation token で読む。上限は、installation（owner）ごとに 1 時間 5,000 回からである（repository と利用者が増えると、12,500 回まで増える）。

| 要求 | REST の回数 | 数える上限 |
|---|---|---|
| どの要求も（認証） | 2 回（主体、Logiforge の App のトークンかの確かめ）＋ 60 秒に 1 回、installation の数 + 1 回（リポジトリの権限。[権限のキャッシュ](../settings/users.md#権限のキャッシュ)） | 主体ごと（App のトークンかの確かめは App） |
| 作業の一覧 | 3 回以上（open の PR と issue の一覧、repository のコメントの一覧）＋ open の PR ごとに 1 回（レビュー） | 主体ごと |
| apply request を読む | 3 回（PR、PR のコメント、レビュー） | 主体ごと |
| 木のノードを読む | 3 回以上（open の PR と issue の一覧、repository のコメントの一覧） | 主体ごと |
| 書き込み | 1〜3 回（PR の作成・merge・close、レビュー、コメント、ラベル） | 主体ごと |
| worker の反映かの判定 | `main` のコミット 1 つに 1 回（`commits/{sha}/pulls`。[重さ](../backend/verify/README.md#重さ)） | installation |
| worker の記録の控え | repository ごとに、記録を読む間隔ごとに 1 回（`issues/comments?since=`。[記録の控え](../backend/verify/records.md)） | installation |
| worker の installation token | owner ごとに、バックエンドが作り直すたびに 2 回（20 分ごと） | App の JWT。installation の上限には数えない |

- worker の `ls-remote` と `fetch` は、REST の上限には数えない。git の操作の、乱用を防ぐ制限には数える（[worker](#worker)）。
- worker の REST は、ほとんどが記録の控えである。
  - 記録を読む間隔を 30 秒にすると、repository 1 つで 1 時間 120 回である。1 つの installation で、repository 30 個ほどまで足りる（[見回りの間隔](../adapters/github.md#見回りの間隔)）。
  - 反映かの判定は、反映の数だけなので、1 日に数十回から 100 回ほどである。
- open の apply request が 50 件あると、作業の一覧 1 回で、読む主体の REST を 50 回ほど使う。
  - 外の仕組みが、1 つの主体で、作業の一覧を repository 10 個について 1 分ごとに読むと、1 時間 3 万回ほどで、その主体の上限を超える。
  - 認証は、エージェントが 1 秒ごとに要求すると、主体ごとに 1 時間 7,000〜1 万回で、上限を超える。
- そのため、1 つの主体が頻繁に読むと、GitHub の API のレート制限に当たる（[GitHub アダプタ](../adapters/github.md)）。
  - 主体ごとの上限なので、人とエージェントが増えても、ほかの主体の読みは止まらない。
