# Logiforge のイメージ。中身は logiforge のバイナリと git だけである。
# コンテナごとに、コマンドを変える（logiforge serve、logiforge worker）。動かし方は design/deploy.md に書く。
# base は digest で固定する。上げるときは、タグと digest を一緒に書き換える。
FROM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/logiforge ./cmd/logiforge

FROM alpine:3.22@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f
# worker は git を使う。credential helper は sh で動く（alpine の sh を使う）。
RUN apk add --no-cache git \
 && adduser -D -H -u 10001 -h /var/lib/logiforge logiforge \
 && mkdir -p /var/lib/logiforge/worker /var/lib/logiforge/store \
 && chown -R 10001:10001 /var/lib/logiforge
COPY --from=build /out/logiforge /usr/local/bin/logiforge
# root では動かさない。
USER 10001:10001
ENV HOME=/var/lib/logiforge
WORKDIR /var/lib/logiforge
EXPOSE 8080
ENTRYPOINT ["logiforge"]
CMD ["serve"]
