# CLI のコマンドの案

CLI を設計するときに、出発点として使う。
CLI の決まりは [CLI](../docs/cli/README.md) が正本である。

## コマンド

```
hv auth login
hv tree view [<node>] [--depth 3] [--json]
hv node view <node>
hv node history <node>

hv story create --parent <hypothesis> --file story.yaml [--ar <ar>]
hv story inactivate <story> --reason "..." [--ar <ar>]
hv verdict submit <hypothesis> --value false --report report.md \
                  [--artifact ./out/result.csv] [--cite <artifact>] \
                  [--cite-subtree <node>@<revision>] [--ar <ar>]
hv node edit <hypothesis> --add-background note.md [--ar <ar>]

hv ar create --file ops.yaml
hv ar list --state open
hv ar view <ar>
hv ar diff <ar>
hv ar review <ar> --approve | --request-changes | --comment --body "..."
hv ar close <ar> --reason "..."

hv artifact upload ./run.py ./run.log

hv thread create --kind pvr --node <hypothesis> --body procedure.md
hv thread list --kind pvr --state open
hv thread view <thread>
hv thread reply <thread> --body "..."
hv thread close <thread>

hv work list [--subtree <node>]        # 着手できる作業
hv work done-check <root>              # 作業の終わりの判定
```

- `--ar` を付けると、既存の apply request に操作を加える。付けないと、新しい apply request を作る。
