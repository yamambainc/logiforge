# WebUI

人間が Logiforge を使う画面の決まりを確かめるときに読む。
WebUI を使うのは、利用者である。利用者ができることは、[役割](../roles/README.md) に書く。

## 画面

- [repository の一覧](repositories.md)：repository を選ぶ。
- [木のビューア](tree-viewer.md)：木と、各ノードの詳細を見る。
- [人への依頼](human-requests.md)：選んだ repository の人への依頼を一覧し、回答する。
- [apply request と履歴](history.md)：apply request の差分とレビューと、反映したあとの木を見る。過去の時点の木を見る。
- [ダッシュボード](dashboard.md)：作業の状況を見る。
- [セットアップ](setup.md)：Logiforge を初めて起動したときに、GitForge とのつなぎ方と最初の利用者を決める。
- [設定](settings.md)：利用者が、repository を足し、GitForge とのつなぎ方を変える。

## 共通の決まり

- すべての画面を、デスクトップのブラウザでも、スマートフォンの幅（375 px）でも使える。
  - なぜ：利用者は、外出先でも、人への依頼に答えたり、共有された URL を開いてスナップショットツリーを確かめたりするから。
- 木の変更（ストーリーの作成、検証結果の提出など）は、WebUI からは行わない。
  - 例外は、repository の作成（根の主文の作成）である（[設定](settings.md#repository-を足す)）。
  - なぜ：木を変えるのは opener の役割で、opener は CLI を使うから（[役割](../roles/README.md)）。
- 利用者とエージェントが書く次の文章は、Markdown として示す。
  - 検証結果のレポート
  - 人への依頼の本文と回答
  - レビューと議論
  - 背景情報
  - ストーリーの概要
  - Markdown の表、コード、引用、チェックリストを示す。
  - Mermaid のコードは、図にする。
  - 主文とタイトルは、1 行の文字として示す。
  - 文章の中の HTML とスクリプトは、動かさない。
