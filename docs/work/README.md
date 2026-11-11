# 着手できる作業と通知

opener や reviewer が、次にする作業を見つけるときに読む。
作業の割り当てとループは、forge では行わない。forge が提供するのは、問い合わせと通知の 2 つだけである。

## 着手できる作業

forge は、いま着手できる作業を返す問い合わせを提供する。
GitHub の `gh pr list --search review-requested` に相当する。

問い合わせは、次のものを返す。

- 未決の葉（[葉と検証結果](../tree/README.md)）
- 要再検討の仮説（[真偽値の伝播](../tree/propagation.md)）
- レビュー待ちの apply request（[apply request](../apply-request/README.md)）
- 返信待ちのスレッド（[スレッド](../threads/README.md)）
- 引用元が変化した検証結果（[検証結果と引用](../tree/verdict.md)）

問い合わせの範囲は、次のとおり。

- 返すのは、active なノードに関わる作業だけである。
  - なぜ：inactive な部分木は、伝播に関与しないから（[inactive 化](../tree/inactive.md)）。
- 部分木を指定して、その中の作業だけを問い合わせられる。

## 作業の終わりの判定

1 つの木の作業が終わったかどうかは、次の問い合わせで判定する。

- 根の値が決まっている。
  - 根の値が決まるとは、true か false になることをいう。
- 根拠部分木で、着手できる作業が 0 件である。
  - 根拠部分木は、[真偽値の伝播](../tree/propagation.md) に定義がある。
  - なぜ：引用元が変化しても値は自動では変えないので、根拠部分木の見直しを終えるまでは結論を確定できないから。

## 通知

forge は、次のイベントを webhook で通知する。

- apply request が open になった。
- apply request が更新された。
- apply request が反映された。
- apply request が close された。
- スレッドが作られた。
- スレッドに返信が来た。
- 引用元が変化した。

## ループ

- 作業のループは、既存の仕組みで回す。
  - 例：Claude Code の /loop、cron、GitHub Actions の schedule
- それらの仕組みの使い方は、この仕様には書かない。

## 対応しないこと

- 作業を特定の主体に割り当てること
- 作業の担当を付け替えること
- 作業の優先度を決めること
