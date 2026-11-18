# apply request の向き先

apply request の向き先（pull request の base のブランチ）を、バックエンドがどう扱うかを確かめるときに読む。
pull request との対応は [apply request](README.md) に書く。

## 向き先

- apply request の向き先は、pull request の base のブランチである。
- バックエンドが作る pull request の向き先は、いつも `main` である。作成の API に、向き先を指定するものはない（[作成](create.md#作成)）。
  - ほかの apply request のブランチに積み上げたブランチも、`main` 向きの apply request になる。差分は、`main` との分かれた点からなので、下の apply request の変更も含む。
- GitForge の画面などで、pull request の base を `main` 以外に変えることはできる。Logiforge は、それを止めない。
- Logiforge が検査し、反映するのは、向き先が `main` の apply request だけである。
  - なぜ：向き先が `main` でない pull request は、`main` に入らないから。その変更は、いずれ `main` に向かう apply request になったときに、そこで検査される。
  - 向き先のブランチへの merge は、GitForge で起きる。Logiforge は、止めないし、記録もしない。

## 向き先が main でない apply request

- バックエンドは、提案を読むときに、向き先を GitForge から読み、apply request の `target` として返す（[GitForge のポート](../ports/gitforge.md)）。
- 操作の一覧は返さない（空にする）。差分を検査しないからである。
- 差分と先頭のコミットの一覧は、ほかの apply request と同じく返す。
- レビューは、GitForge に記録する。`approve` しても、反映の手順に進まない。応答の警告に、Logiforge の反映の対象でないことを書く（[反映の手順](../merge.md)）。
- レビューの材料は返さず、`overlay_unsupported`（422）を返す。
- 反映したあとのスナップショットツリーは、向き先のブランチの今の先頭に重ねて返す。検査はしない（[向き先に重ねる](../snapshot/branch.md#向き先に重ねる)）。
  - なぜ返すか：利用者が、積み上げた apply request を、下の apply request に入れたあとの姿で確かめられるようにするため。
- 着手できる作業と、作業の終わりの判定には数えない（[計算](../compute.md)）。

## 向き先を変えたとき

- 向き先は、読むたびに GitForge から読む。そのため、向き先を変えると、次に読んだときから、変えたあとの向き先で扱う。
- `main` に変えたら、ほかの apply request と同じく、差分を検査し、レビューと反映の対象になる。差分は、もともと `main` との分かれた点からのものである。
- `main` に変えたあとは、`approve` し直すと反映する。それまでの `approve` では反映していないので、レビュー待ちになる（[計算](../compute.md)）。
- `main` から変えたら、それまでのレビューと反映の失敗の記録は残るが、反映の対象から外れる。
