#!/usr/bin/env bash
# compose.yaml の Forgejo に、Logiforge が使う organization、team、ユーザーとトークンを作り、
# 秘密を .compose/secrets/ に書く。初めの 1 回だけ動かす。
# 使い方：scripts/compose-setup.sh
# 終わると、利用者（alice）のトークンを .compose/alice_token に書く。
set -euo pipefail
cd "$(dirname "$0")/.."
dir=.compose/secrets
if [ -e "$dir/writer_token" ] && [ -s "$dir/writer_token" ]; then
  echo "$dir はもうある。作り直すときは、docker compose down -v と rm -rf .compose をしてから動かす" >&2
  exit 1
fi
# 乱数の 20 バイトを 16 進で。od は読み終えて止まるので、pipefail で途中の pipe が切れて落ちることがない。
rand() { od -An -tx1 -N20 /dev/urandom | tr -d ' \n'; }

# 秘密のファイルは、compose がサービスを作る前にそろっている必要がある。先に作り、トークンはあとで書く。
# コンテナのユーザー（uid 10001）が読めるよう、ファイルは 644、ディレクトリは 700 にする。
mkdir -p "$dir"
chmod 700 .compose "$dir"
echo "k1=$(rand)" >"$dir/record_keys"
: >"$dir/writer_token"
: >"$dir/worker_token"
chmod 644 "$dir"/*

docker compose up -d --wait forgejo
fj() { docker compose exec -T -u git forgejo forgejo --config /data/gitea/conf/app.ini "$@"; }
api=http://localhost:3000/api/v1

# admin のパスワードは、最後に setup のトークンを消すとき（Forgejo は、トークンの操作に basic 認証を求める）にだけ使う。
adminpw=$(rand)
fj admin user create --admin --username logiforge-admin --password "$adminpw" --email admin@example.com --must-change-password=false >/dev/null
admin=$(fj admin user generate-access-token --username logiforge-admin --token-name setup --scopes all --raw)
call() { # call <method> <path> [<json>]
  curl -fsS -X "$1" -H "Authorization: token $admin" -H 'Content-Type: application/json' ${3:+-d "$3"} "$api$2"
}

call POST /orgs '{"username":"logiforge","visibility":"private"}' >/dev/null
team() { # team <name> <permission> <units_map> <can_create_org_repo>。team の id を返す
  call POST /orgs/logiforge/teams "{\"name\":\"$1\",\"permission\":\"$2\",\"units_map\":$3,\"includes_all_repositories\":true,\"can_create_org_repo\":$4}" |
    grep -o '"id":[0-9]*' | head -1 | cut -d: -f2
}
writers=$(team writers write '{"repo.code":"write","repo.pulls":"write","repo.issues":"write"}' true)
users=$(team logiforge-users read '{"repo.code":"write","repo.pulls":"read","repo.issues":"read"}' false)
readers=$(team readers read '{"repo.code":"read"}' false)

user() { # user <login> <team の id> <scopes>。トークンを返す
  fj admin user create --username "$1" --random-password --email "$1@example.com" --must-change-password=false >/dev/null
  call PUT "/teams/$2/members/$1" >/dev/null
  fj admin user generate-access-token --username "$1" --token-name logiforge --scopes "$3" --raw
}
user logiforge-writer "$writers" all >"$dir/writer_token"
# worker は、読むだけのトークンを使う。
user logiforge-reader "$readers" read:repository,read:organization >"$dir/worker_token"
user alice "$users" all >.compose/alice_token
chmod 600 .compose/alice_token

# setup に使った admin のトークン（scope は all）は、もう要らないので消す。
curl -fsS -X DELETE -u "logiforge-admin:$adminpw" "$api/users/logiforge-admin/tokens/setup" >/dev/null
echo "用意した。次に docker compose up -d --wait で、worker とバックエンドを動かす。"
echo "利用者 alice のトークンは .compose/alice_token にある。"
