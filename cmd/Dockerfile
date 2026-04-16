from golang:1.24.4-alpine as builder

workdir /app

copy . .

run go build -v -ldflags "-s -w" -o proxyd ./cmd

from alpine:3.23.4

copy ./cmd/etc/proxyd /etc/proxyd
copy --from=builder /app/proxyd /usr/bin/proxyd

run apk add ca-certificates

cmd  ["/usr/bin/proxyd"]
