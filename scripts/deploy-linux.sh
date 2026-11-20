#!/bin/sh
# 手元の checkout から logiforge をビルドし、Linux のサーバのバイナリを入れ替えて、serve と worker を起動し直す。
# 使い方と前提は docs/adoption/linux-host/README.md の「更新」に書く。
#
#   scripts/deploy-linux.sh <ssh の送り先>      例：scripts/deploy-linux.sh admin@logiforge-host
#
# - 今のブランチのまま、今のコミットでビルドする（git の switch や pull はしない）。ビルドするコミットを、最初に示す。
# - 版とコミットは、Dockerfile と CI と同じく、git describe と git rev-parse で埋め込む。
# - 送り先では sudo を使う（パスワードを聞かれることがある）。

# 送り先で動かすコマンドは、手元で組み立てる（一時ファイルのパスを埋め込む）。
# shellcheck disable=SC2029
set -eu

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
	echo "使い方：$0 <ssh の送り先>（例：$0 admin@logiforge-host）" >&2
	exit 2
fi
HOST=$1

cd "$(git rev-parse --show-toplevel)"

VERSION=$(git describe --tags --always --dirty)
COMMIT=$(git rev-parse HEAD)
echo "ビルドする：$(git rev-parse --abbrev-ref HEAD) の $VERSION（$COMMIT）"
if [ -n "$(git status --porcelain)" ]; then
	echo "注意：コミットしていない変更も、ビルドに入る。" >&2
fi

MACHINE=$(ssh "$HOST" uname -m)
case $MACHINE in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*)
	echo "送り先の CPU（$MACHINE）に合うビルドがない。amd64 か arm64 だけ。" >&2
	exit 1
	;;
esac

LOCAL_TMP=$(mktemp -d)
REMOTE_TMP=
# trap から呼ぶ
# shellcheck disable=SC2329
cleanup() {
	rm -rf "$LOCAL_TMP"
	if [ -n "$REMOTE_TMP" ]; then
		ssh "$HOST" "rm -f '$REMOTE_TMP'" || true
	fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM

(cd webui && npm ci && npm run build)
CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -trimpath \
	-ldflags="-s -w -X github.com/yamambainc/logiforge/internal/version.Version=$VERSION -X github.com/yamambainc/logiforge/internal/version.Commit=$COMMIT" \
	-o "$LOCAL_TMP/logiforge" ./cmd/logiforge

REMOTE_TMP=$(ssh "$HOST" mktemp /tmp/logiforge.XXXXXX)
scp -q "$LOCAL_TMP/logiforge" "$HOST:$REMOTE_TMP"
# restart は、古いプロセスを止めてから新しいプロセスを起こす。worker が 2 つ同時に動くことはない
ssh -t "$HOST" "sudo install -m 0755 '$REMOTE_TMP' /usr/local/bin/logiforge && sudo systemctl restart logiforge-worker logiforge-serve"

# 起動してすぐ落ちるときに気づけるように、少し待ってから確かめる
sleep 3
if ssh "$HOST" systemctl is-active logiforge-serve logiforge-worker; then
	echo "入れ替えた：$VERSION（$COMMIT）"
	exit 0
fi
echo "serve か worker が動いていない。ログを見る：ssh -t $HOST sudo journalctl -u logiforge-serve -u logiforge-worker -n 100 --no-pager" >&2
exit 1
