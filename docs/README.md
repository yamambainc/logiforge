# Logiforge の仕様

Logiforge が何を満たすかを確かめるときに読む。
設計と実装の判断は、ここに書いた決まりに従う。

- 仕様の入口は、このページである。
- [index.html](index.html) は、clone した手元のブラウザで開く概要ページである。

## design へのリンク

- 要件のページ（`tree/`、`apply-request/`、`webui/` など）は、[design/](../design/README.md) を指さない。
  - なぜ：design が要件に従うので、要件が design に頼ると、依存が両向きになるから。
- 導入と運用の手順（[install](install.md)、[adoption/](adoption/README.md)、[development/](development/README.md)）は、design を指してよい。
  - なぜ：手順の理由や環境変数の正本は design にあり、写さずにリンクで指すため。

## 読む順

1. [目的と範囲](concepts/README.md)
2. [用語](concepts/glossary.md)
3. [仮説とストーリーの木](tree/README.md)
4. [真偽値の伝播](tree/propagation.md)
5. [検証結果と引用](tree/verdict.md)
6. [仮説が偽になったとき](tree/falsification.md)
7. [inactive 化](tree/inactive.md)
8. [apply request](apply-request/README.md)
9. [論理レビュー](apply-request/review.md)
10. [論理レビューの審査観点](apply-request/review-criteria.md)
11. [壊れた repository を直す](apply-request/repair.md)
12. [役割](roles/README.md)
13. [着手できる作業](work/README.md)
14. [人への依頼（PVR と判断の依頼）](human-requests/README.md)
15. [CLI](cli/README.md)
16. [WebUI](webui/README.md)
17. [全体構成](architecture/README.md)

## 使い始める

仕様ではなく、導入の手順である。

- [インストール](install.md)：バイナリを取り、動かす
- [既存のプロジェクトへの導入](adoption/README.md)：使い方から置き方を選び、Logiforge を導入する

## 開発する

仕様ではなく、コードを変える人の手順である。

- [開発](development/README.md)：パッケージ、テスト、WebUI のビルド、compose、リリース

## 仕様の外にあるもの

- 決まっていない論点は、このリポジトリの issue で扱う。
