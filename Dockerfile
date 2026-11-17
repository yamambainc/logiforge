# Logiforge のイメージ。中身は logiforge のバイナリ（WebUI を含む）と git だけである。
# コンテナごとに、コマンドを変える（logiforge serve、logiforge worker）。動かし方は design/deploy.md に書く。
# base は digest で固定する。上げるときは、タグと digest を一緒に書き換える。

# WebUI をビルドする（design/webui/stack.md の「配り方」）。出力は webui/dist/ で、Go の段が embed する。
FROM node:22-alpine@sha256:1314fb30ec37592d5845e5a160d522c0e680972aa534549940b8d73bde88481a AS webui
WORKDIR /src/webui
COPY webui/package.json webui/package-lock.json ./
RUN npm ci
COPY webui/ ./
RUN npm run build

FROM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS build
# logiforge version に出す版とコミット（internal/version）。compose と CI が、git describe と git rev-parse の値を渡す
ARG VERSION=dev
ARG COMMIT=
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY webui/embed.go ./webui/
COPY --from=webui /src/webui/dist ./webui/dist
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/yamambainc/logiforge/internal/version.Version=${VERSION} -X github.com/yamambainc/logiforge/internal/version.Commit=${COMMIT}" \
    -o /out/logiforge ./cmd/logiforge

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
