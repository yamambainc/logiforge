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
7. [アダプタ](adapters/README.md)
8. [独自フォージ](forge/README.md)
9. [API](api/README.md)
