# 仮説検証ハーネスの仕様

仮説検証ハーネスが何を満たすかを確かめるときに読む。
設計と実装の判断は、ここに書いた決まりに従う。

- 仕様の入口は、このページである。
- [index.html](index.html) は、clone した手元のブラウザで開く概要ページである。

## 読む順

1. [目的と範囲](concepts/README.md)
2. [用語](concepts/glossary.md)
3. [仮説とストーリーの木](tree/README.md)
4. [真偽値の伝播](tree/propagation.md)
5. [検証結果と引用](tree/verdict.md)
6. [仮説が偽になったとき](tree/falsification.md)
7. [inactive 化](tree/inactive.md)
8. [Apply Request](apply-request/README.md)
9. [Reviewer の審査観点](apply-request/review.md)
10. [役割とハーネスの機能](agents/README.md)
11. [スケジューラ](agents/scheduler.md)
12. [スレッド（依頼と PVR）](threads/README.md)
13. [CLI](cli/README.md)
14. [WebUI](webui/README.md)
15. [全体構成](architecture/README.md)

## 仕様の外にあるもの

- 決まっていない論点は、このリポジトリの GitHub issue で扱う。
- 設計の出発点にする案は、[memo/](../memo/README.md) に置く。
  - memo/ は仕様ではない。
