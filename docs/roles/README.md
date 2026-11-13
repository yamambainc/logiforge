# 役割

誰が何をできるかを確かめるときに読む。

## opener

opener は、[apply request](../apply-request/README.md) を出す主体である。

- CLI を使える主体なら、どのエージェントでも、人でも opener になれる。
- opener がすることの例は、次のとおり。
  - 問いを、ストーリーと子仮説に分解する。
  - 葉を検証する（文献検索、プログラムの実装と実行）。
  - 計算機の中で検証できない葉について、[PVR](../human-requests/README.md) を起票する。
  - 人への依頼の回答を、検証結果に変換する。
  - 要再検討の仮説に対応する（[仮説が偽になったとき](../tree/falsification.md)）。
  - ストーリーの論理の欠陥に気づいたら、`story.inactivate` を出す。

木を作る主体と、葉を検証する主体は、分けない。

- なぜ：同じ主体が両方を担っても、審査が別のコンテキストで行われる限り、問題が生じないから。

## reviewer

reviewer は、apply request を審査する主体である。
reviewer の決まりは、[論理レビュー](../apply-request/review.md) に書く。

## 利用者

利用者は、[WebUI](../webui/README.md) を使う人間である。
利用者ができることは、次の 2 つだけである。

- repository を作る（根の仮説の主文を書く）。
- [人への依頼](../human-requests/README.md) に回答する（PVR の結果の記入、判断の記入）。

人への依頼には、問いを持ち込んだ人とは別の利用者（実験の担当者など）も回答できる。

## 対応しないこと

- 特定のエージェントを、opener や reviewer に固定しない。
- 誰がいつ作業するかは、この仕様では決めない。
  - 作業の探し方は、[着手できる作業](../work/README.md) に書く。
- repository ごとの読み取りの制限
  - [Forge](../concepts/glossary.md) が repository ごとの読み取りの権限を持つときは、それに従う。
