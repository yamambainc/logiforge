# Logiforge のバックエンド設計

Logiforge のバックエンドを実装するときに読む。
要件は [docs/](../docs/README.md) が正本である。この設計は要件に従い、要件の内容は写さずにリンクで指す。

## 読む順

1. [レイヤー構成](architecture.md)
2. [木のファイル配置](files/README.md)
3. [バックエンド](backend/README.md)
   - [apply request の作成と更新](backend/apply-request.md)
4. [ポート](backend/ports/README.md)
5. [スナップショット](backend/snapshot.md)
6. [worker](backend/worker.md)
   - [worker の内部 API](backend/worker-api.md)
7. [アダプタ](adapters/README.md)
8. [API](api/README.md)
9. [CLI](cli/README.md)

## 用語

- **Forge**：git のホスティング、pull request、レビュー、issue、主体とトークンを持つ、外のサービス（[用語](../docs/concepts/glossary.md)）。
  - バックエンドは、Forge を [Forge のポート](backend/ports/forge.md) を通して使う。どの Forge を使うかは、[アダプタ](adapters/README.md) で差し替える。
  - Forge ごとに違う振る舞いは、バックエンドの文書では「Forge の性質」として書く。具体的な対応は、[アダプタ](adapters/README.md) の各ページに書く。
