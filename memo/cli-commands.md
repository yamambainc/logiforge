# CLI のコマンドの案

CLI を設計するときに、出発点として使う。
CLI の決まりは [CLI](../docs/cli/README.md) が正本である。

## コマンド

```
logiforge auth login
logiforge tree view [<node>] [--depth 3] [--json]
logiforge node view <node>
logiforge node history <node>

logiforge story create --parent <hypothesis> --file story.yaml [--apply-request <apply-request>]
logiforge story inactivate <story> --reason "..." [--apply-request <apply-request>]
logiforge verdict submit <hypothesis> --value false --report report.md \
                         [--artifact ./out/result.csv] [--cite <artifact>] \
                         [--cite-subtree <node>@<revision>] [--apply-request <apply-request>]
logiforge node edit <hypothesis> --add-background note.md [--apply-request <apply-request>]

logiforge apply-request create --file ops.yaml
logiforge apply-request list --state open
logiforge apply-request view <apply-request>
logiforge apply-request diff <apply-request>
logiforge apply-request review-material <apply-request>   # レビューの材料
logiforge apply-request review <apply-request> --approve | --request-changes | --comment --body "..."
logiforge apply-request close <apply-request> --reason "..."

logiforge artifact upload ./run.py ./run.log

logiforge thread create --kind pvr --node <hypothesis> --body procedure.md
logiforge thread list --kind pvr --state open
logiforge thread view <thread>
logiforge thread reply <thread> --body "..."
logiforge thread close <thread>

logiforge work list [--subtree <node>]        # 着手できる作業
logiforge work done-check <root>              # 作業の終わりの判定
```

- `--apply-request` を付けると、既存の apply request に操作を加える。付けないと、新しい apply request を作る。
