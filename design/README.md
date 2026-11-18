# Logiforge のバックエンド設計

Logiforge のバックエンドを実装するときに読む。
要件は [docs/](../docs/README.md) が正本である。この設計は要件に従い、要件の内容は写さずにリンクで指す。

## 読む順

1. [レイヤー構成](architecture.md)
2. [木のファイル配置](files/README.md)
3. [バックエンド](backend/README.md)
   - [apply request](backend/apply-request/README.md)
4. [ポート](backend/ports/README.md)
5. [スナップショット](backend/snapshot/README.md)
6. [worker](backend/worker/README.md)
   - [反映かの判定](backend/verify/README.md)
   - [壊れた repository](backend/broken.md)
7. [アダプタ](adapters/README.md)
8. [API](api/README.md)
9. [CLI](cli/README.md)
10. [WebUI](webui/README.md)
11. [設定](settings/README.md)
12. [動かし方（コンテナ）](deploy/README.md)
13. [費用と規模の見積もり](estimates/README.md)
14. [ディレクトリ方式](directory/README.md)

## 用語

- **GitForge**：git のホスティング、pull request、レビュー、issue、主体とトークンを持つ、外のサービス（[用語](../docs/concepts/glossary.md)）。
  - バックエンドは、GitForge を [GitForge のポート](backend/ports/gitforge.md) を通して使う。どの GitForge を使うかは、[アダプタ](adapters/README.md) で差し替える。
  - GitForge ごとに違う振る舞いは、バックエンドの文書では「GitForge の性質」として書く。具体的な対応は、[アダプタ](adapters/README.md) の各ページに書く。
