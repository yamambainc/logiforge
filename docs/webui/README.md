# WebUI

人間が forge を使う画面の決まりを確かめるときに読む。
WebUI を使うのは利用者である。利用者ができることは、[役割](../roles/README.md) に書く。

## 画面

- [repository の一覧](repositories.md)：repository を選ぶ。repository を作る。
- [木のビューア](tree-viewer.md)：木と、各ノードの詳細を見る。
- [人への依頼](human-requests.md)：選んだ repository の人への依頼を一覧し、回答する。
- [apply request と履歴](history.md)：apply request の差分と議論を見る。過去の時点の木を見る。
- [ダッシュボード](dashboard.md)：作業の状況を見る。

## 共通の決まり

- 木の変更（ストーリーの作成、検証結果の提出など）は、WebUI からは行わない。
  - 例外は、repository の作成（根の主文の作成）である（[repository の一覧](repositories.md)）。
  - なぜ：木を変えるのは opener の役割で、opener は CLI を使うから（[役割](../roles/README.md)）。
