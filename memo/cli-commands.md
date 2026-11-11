# CLI のコマンドの案

CLI を設計するときに、出発点として使う。
CLI の決まりは [CLI](../docs/cli/README.md) が正本である。

## コマンド

```
logiforge auth login
logiforge tree view [<node>] [--depth 3] [--json]
logiforge node view <node>
logiforge node history <node>

logiforge story create --parent <hypothesis> --file story.yaml [--ar <ar>]
logiforge story inactivate <story> --reason "..." [--ar <ar>]
logiforge verdict submit <hypothesis> --value false --report report.md \
                         [--artifact ./out/result.csv] [--cite <artifact>] \
                         [--cite-subtree <node>@<revision>] [--ar <ar>]
logiforge node edit <hypothesis> --add-background note.md [--ar <ar>]

logiforge ar create --file ops.yaml
logiforge ar list --state open
logiforge ar view <ar>
logiforge ar diff <ar>
logiforge ar review <ar> --approve | --request-changes | --comment --body "..."
logiforge ar close <ar> --reason "..."

logiforge artifact upload ./run.py ./run.log

logiforge thread create --kind pvr --node <hypothesis> --body procedure.md
logiforge thread list --kind pvr --state open
logiforge thread view <thread>
logiforge thread reply <thread> --body "..."
logiforge thread close <thread>

logiforge work list [--subtree <node>]        # 着手できる作業
logiforge work done-check <root>              # 作業の終わりの判定
```

- `--ar` を付けると、既存の apply request に操作を加える。付けないと、新しい apply request を作る。
