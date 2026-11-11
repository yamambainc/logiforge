# webhook

外の仕組みが、forge の変化を受け取るときに読む。
通知するイベントは、[通知](../../docs/work/README.md) が正本である。

## 登録

- `client` は、`POST /webhooks` で受け取り先の URL と、受け取るイベントの種類を登録する。
- 木を指定すれば、その木のイベントだけを受け取る。
- 登録の応答で、署名の秘密鍵を 1 回だけ返す。

## イベント

| 種類 | 送るとき |
|---|---|
| `apply_request.opened` | apply request が open になった |
| `apply_request.updated` | apply request の操作が変わった |
| `apply_request.reviewed` | レビューが付いた |
| `apply_request.merge_failed` | 反映に失敗した |
| `apply_request.merged` | apply request が反映された |
| `apply_request.closed` | apply request が close された |
| `thread.created` | スレッドが作られた |
| `thread.posted` | スレッドに書き込みがあった |
| `citation.changed` | 引用元が変化した |

- `apply_request.reviewed` と `apply_request.merge_failed` は、修正待ちの作業ができたことを opener に知らせるために送る（[着手できる作業](../../docs/work/README.md)）。

## ペイロード

```json
{
  "id": 1042,
  "type": "apply_request.opened",
  "tree_id": "tre_01J...",
  "revision": 1042,
  "occurred_at": "2026-11-11T12:00:00Z",
  "subject": { "kind": "apply_request", "id": "apr_01J..." }
}
```

- `id` は、[イベント](../data/history.md) の `seq` である。
- ペイロードには、変化の中身を入れない。
  - なぜ：webhook は問い合わせ直す合図だから（[通知](../../docs/work/README.md)）。受け取った側は、API で最新の状態を読む。

## 署名

- forge は、本文の HMAC-SHA256 を `Logiforge-Signature: sha256=<16 進>` ヘッダーで送る。
- 受け取った側は、登録のときの秘密鍵で署名を確かめる。

## 届け方

- 同じイベントが、2 回以上届くことがある。
  - 受け取った側は、`id` で重複を除く。
- 届かなかったイベントは、間を空けて送り直す。
- 受け取り損ねても、`GET /events?after=<id>` で読み直せる。
