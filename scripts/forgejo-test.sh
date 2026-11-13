#!/usr/bin/env bash
# 結合テストのための Forgejo を、一時ディレクトリで動かす。
# 使い方：scripts/forgejo-test.sh <forgejo のバイナリ> <ポート>
# 標準出力に、テストに渡す環境変数（FORGEJO_URL、FORGEJO_ADMIN_TOKEN、FORGEJO_PID、FORGEJO_WORK）を書く。
set -euo pipefail
bin=$1
port=${2:-3999}
work=$(mktemp -d)
mkdir -p "$work/custom/conf"
cat > "$work/custom/conf/app.ini" <<INI
APP_NAME = logiforge-test
RUN_MODE = prod
WORK_PATH = $work
[server]
HTTP_ADDR = 127.0.0.1
HTTP_PORT = $port
ROOT_URL = http://127.0.0.1:$port/
DISABLE_SSH = true
OFFLINE_MODE = true
[database]
DB_TYPE = sqlite3
PATH = $work/forgejo.db
[repository]
ROOT = $work/repos
[security]
INSTALL_LOCK = true
[service]
DISABLE_REGISTRATION = true
[log]
MODE = file
LEVEL = Warn
ROOT_PATH = $work/log
[attachment]
MAX_SIZE = 10
[git]
HOME_PATH = $work/git-home
INI
# 手元の git の設定を、Forgejo に使わせない。
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM GIT_CONFIG_NOSYSTEM
export FORGEJO_WORK_DIR=$work
"$bin" --work-path "$work" --config "$work/custom/conf/app.ini" migrate >/dev/null
"$bin" --work-path "$work" --config "$work/custom/conf/app.ini" admin user create --admin --username root --password 'root-password-1' --email root@example.com --must-change-password=false >/dev/null
token=$("$bin" --work-path "$work" --config "$work/custom/conf/app.ini" admin user generate-access-token --username root --token-name test --scopes all --raw)
nohup "$bin" --work-path "$work" --config "$work/custom/conf/app.ini" web >"$work/web.log" 2>&1 &
pid=$!
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:$port/api/v1/version" >/dev/null 2>&1; then
    echo "FORGEJO_URL=http://127.0.0.1:$port"
    echo "FORGEJO_ADMIN_TOKEN=$token"
    echo "FORGEJO_PID=$pid"
    echo "FORGEJO_WORK=$work"
    exit 0
  fi
  sleep 0.2
done
echo "Forgejo が起動しなかった" >&2
cat "$work/web.log" >&2
exit 1
