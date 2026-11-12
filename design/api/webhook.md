# webhook

外の仕組みが、forge の変化を受け取るときに読む。
通知の原則は、[通知](../../docs/work/README.md) が正本である。

## 登録

- `client` は、project ごとに、受け取り先の URL と、受け取るイベントの種類を登録する。
  - 登録の API は、`/projects/{project}/webhooks` である。
- 登録の応答で、署名の秘密鍵を 1 回だけ返す。

## イベント

イベントは、変化した対象の種類ごとに 1 つずつ、次の 3 種類だけにする。

| 種類 | 送るとき |
|---|---|
| `apply_request.changed` | apply request が作られた、更新された、レビューが付いた、反映された、反映に失敗した、close された |
| `human_request.changed` | 人への依頼が起票された、回答された |
| `citation.changed` | 有効な検証結果が引用している部分木で、引用元が変化した（[引用元の変化](../../docs/tree/verdict.md)） |

- 細かい種類に分けない。
  - なぜ：受け取った側は、どの変化でも同じように問い合わせ直すから。
- `citation.changed` は、反映によって、どの検証結果の引用元が変化したかを知らせるために送る。
  - なぜ：引用元の変化は、変化したノードとは別の場所（引用している検証結果）の作業を生むから。
- プロバイダは、細かい種類でイベントを記録してよい（[独自プロバイダ](../providers/native.md)）。

## ペイロード

```json
{
  "id": 1042,
  "type": "apply_request.changed",
  "project": "question-a",
  "revision": "3f1c9a0e5b7d2c4a6e8f0b1d3c5a7e9f1b3d5c7a",
  "occurred_at": "2026-11-11T12:00:00Z",
  "subject": { "kind": "apply_request", "id": "apr_01J..." }
}
```

- `id` は、project の中で 1 から順に増える番号である。
- `revision` は、そのときの木のリビジョンである。
- ペイロードには、変化した対象の種類と識別子だけを入れる。
  - なぜ：webhook は、問い合わせ直す合図だから（[通知](../../docs/work/README.md)）。

## 署名

- forge は、本文の HMAC-SHA256 を `Logiforge-Signature: sha256=<16 進>` ヘッダーで送る。
- 受け取った側は、登録のときの秘密鍵で署名を確かめる。

## 届け方

- 同じイベントが、2 回以上届くことがある。
  - 受け取った側は、`id` で重複を除く。
- 届かなかったイベントは、間を空けて送り直す。
- 受け取り損ねても、`GET /projects/{project}/events?after=<id>` で読み直せる。
