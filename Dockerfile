# Logiforge のイメージ。中身は logiforge のバイナリと git だけである。
# コンテナごとに、コマンドを変える（logiforge serve、logiforge worker）。動かし方は design/deploy.md に書く。
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/logiforge ./cmd/logiforge

FROM alpine:3.22
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
