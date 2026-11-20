# 直し終えたと申告する

opener が、修正待ちの apply request を直し終えたときに、CLI から呼ぶ。

## 呼び出し

- `POST /repositories/{repository}/apply-requests/{applyRequestId}/fixed`
- operationId：`markApplyRequestFixed`

## すること

- pull request のラベルを、修正待ちからレビュー待ちに付け替える（[レビュー待ちと修正待ちのラベル](../../../adapters/pull-requests.md#レビュー待ちと修正待ちのラベル)）。
- 呼べるのは、pull request の作者（opener）だけである。違えば `forbidden` にする。
  - なぜ：直し終えたかを知っているのは、直す人だから。
- open でなければ `not_open` にする。すでにレビュー待ちなら、何もせずに成功とする。
- push はしない。直したコミットは、先に同じブランチに push しておく。

## 関係する要件

- [着手できる作業](../../../../docs/work/README.md)

## 詳細

パラメータ、スキーマ、エラーは、[openapi.yaml](../../openapi.yaml) の `markApplyRequestFixed` に書く。
