#!/usr/bin/env bash
# compose.yaml の Forgejo に、Logiforge が使う organization、team、ユーザーとトークンを作り、
# 秘密を .compose/secrets/ に書く。初めの 1 回だけ動かす。
# 使い方：scripts/compose-setup.sh
# 終わると、利用者（alice）のトークンを .compose/alice_token に、WebUI にサインインするパスワードを .compose/alice_password に書く。
set -euo pipefail
cd "$(dirname "$0")/.."
dir=.compose/secrets
if [ -s "$dir/shared_key" ]; then
  echo "$dir はもうある。作り直すときは、docker compose down -v と rm -rf .compose をしてから動かす" >&2
  exit 1
fi
# 乱数の 20 バイトを 16 進で。od は読み終えて止まるので、pipefail で途中の pipe が切れて落ちることがない。
rand() { od -An -tx1 -N20 /dev/urandom | tr -d ' \n'; }

# 秘密のファイルは、compose がサービスを作る前にそろっている必要がある。先に作り、トークンはあとで書く。
# コンテナのユーザー（uid 10001）が読めるよう、ファイルは 644、ディレクトリは 700 にする。
mkdir -p "$dir"
chmod 700 .compose "$dir"
# 設定の DB の秘密を暗号化する 2 つの鍵（32 バイトの乱数の base64。design/settings/secrets.md）。
head -c 32 /dev/urandom | base64 >"$dir/backend_key"
head -c 32 /dev/urandom | base64 >"$dir/shared_key"
# 644 なので、このホストのほかのユーザーも鍵を読める。開発のための compose だけの形である。本番では、鍵を DB と別の場所に、読める人を絞って置く（design/settings/secrets.md の「鍵を置く場所」）。
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
# 利用者は repository を作り、issue と pull request にコメントを書く。エージェント（opener と reviewer）は、コード、issue、
# pull request を書き、reviewer は merge する（design/adapters/forgejo.md の「主体」）。Logiforge に、書き込み主体はない。
users=$(team logiforge-users write '{"repo.code":"read","repo.pulls":"write","repo.issues":"write"}' true)
agents=$(team agents write '{"repo.code":"write","repo.pulls":"write","repo.issues":"write"}' false)
readers=$(team readers read '{"repo.code":"read","repo.pulls":"read","repo.issues":"read"}' false)

user() { # user <login> <team の id> <scopes>。トークンを返す
  fj admin user create --username "$1" --random-password --email "$1@example.com" --must-change-password=false >/dev/null
  call PUT "/teams/$2/members/$1" >/dev/null
  fj admin user generate-access-token --username "$1" --token-name logiforge --scopes "$3" --raw
}
# worker は、読むだけのトークンを使う。
worker_token=$(user logiforge-reader "$readers" read:repository,read:organization,read:issue)
user alice "$users" all >.compose/alice_token
chmod 600 .compose/alice_token
# エージェントの主体。CLI で、ブランチの push、apply request とレビューに使う。
user agent "$agents" all >.compose/agent_token
user reviewer "$agents" all >.compose/reviewer_token
chmod 600 .compose/agent_token .compose/reviewer_token
# WebUI には、Forgejo にログインしてサインインする。alice のパスワードを決めて、手元に置く。
alicepw=$(rand)
fj admin user change-password --username alice --password "$alicepw" --must-change-password=false >/dev/null
echo "$alicepw" >.compose/alice_password
chmod 600 .compose/alice_password

# WebUI のサインインに使う OAuth2 のアプリケーション（confidential client）を登録する（design/webui/auth.md）。
# redirect_uri は、設定の webui_url に /oauth/callback を足したものと完全に一致させる。
app=$(call POST /user/applications/oauth2 '{"name":"logiforge-webui","redirect_uris":["http://localhost:8080/oauth/callback"],"confidential_client":true}')
client_id=$(echo "$app" | grep -o '"client_id":"[^"]*"' | cut -d'"' -f4)
client_secret=$(echo "$app" | grep -o '"client_secret":"[^"]*"' | cut -d'"' -f4)

# 設定を、設定の DB に書く（初回のセットアップのかわり。design/settings/README.md）。
docker compose run -T --rm --no-deps backend settings load - <<JSON
{
  "gitforge": "forgejo",
  "webui_url": "http://localhost:8080",
  "forgejo": {
    "url": "http://localhost:3000",
    "org": "logiforge",
    "user_team": "logiforge-users",
    "worker_login": "logiforge-reader",
    "worker_token": "$worker_token",
    "oauth_client_id": "$client_id",
    "oauth_client_secret": "$client_secret"
  },
  "record_keys": [{"id": "k1", "secret": "$(rand)"}]
}
JSON

# setup に使った admin のトークン（scope は all）は、もう要らないので消す。
curl -fsS -X DELETE -u "logiforge-admin:$adminpw" "$api/users/logiforge-admin/tokens/setup" >/dev/null
echo "用意した。次に docker compose up -d --wait で、worker とバックエンドを動かす。"
echo "利用者 alice のトークンは .compose/alice_token に、エージェントのトークンは .compose/agent_token と .compose/reviewer_token にある。"
echo "WebUI は http://localhost:8080/ で、alice とパスワード（.compose/alice_password）でサインインする。"
