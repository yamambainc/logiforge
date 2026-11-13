---
name: docs-writing-md
description: markdown の文書（仕様書・業務要求・シーケンス図・README など）を書く・直すときの、md に固有の規則。共通の原則は先に docs-writing を読む。
---

# markdown の書き方

共通の原則は [docs-writing](../docs-writing/SKILL.md) に従う。ここには md に固有の規則だけを書く。

## 正本

- DDL・マイグレーションの SQL を仕様書に書かない。列は表で書き、定義は実装リポの schema を指す。
  - なぜ：メンテコスト。schema と二重になる。

## 構成

- 1 ファイル 100 行程度に収める。超えるなら分ける。
  - なぜ：認知負荷。長いページは読むのもレビューもつらい。
- ファイルパスから中身がわかる粒度で分け、関係の深いものはディレクトリでまとめる。
  - 悪い：`api/report-export-create.md`、`api/report-export-download.md` が他の API と同じ階層に並ぶ
  - 良い：`api/report-export/create.md`、`api/report-export/download.md`
- ファイル名・ディレクトリ名に連番を付けない。読む順は各ディレクトリの `README.md` に書く。
  - なぜ：メンテコスト。追加や並べ替えのたびに改名とリンクの直しが要る。
- シーケンス図は 1 つの流れに 1 つ。図は mermaid で書く。
- mermaid の図も、[docs-writing](../docs-writing/SKILL.md) の「図」に従う。
  - 違いが少しだけなら `alt` で分ける。`alt` だらけになるなら図を分ける。
  - 別の主体の流れは描き直さず、その図へリンクする。
- 調べたことのメモは仕様のディレクトリに置かず、`memo/` などに分ける。
- 画面の仕様には、最低限ワイヤフレームを付ける。

## README

- README には、ずっと変わらないことだけを書く。初回だけの手順や依頼は、PR 本文か issue に書く。

## チェックリスト

書き終えたら、変えたファイルについて確かめる。[docs-writing](../docs-writing/SKILL.md) の共通のチェックリストも実行する。

- [ ] 100 行程度に収まっている（`wc -l`）。
