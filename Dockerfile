# syntax=docker/dockerfile:1
# MicroDeviceStatus server — static Go binary + SQLite volume.
# Single replica only (in-memory sessions). Put HTTPS in front.

FROM golang:1.25-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN go test ./... \
 && go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/microdevicestatus .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates su-exec wget \
 && addgroup -g 1000 mds \
 && adduser -D -u 1000 -G mds mds \
 && mkdir -p /data \
 && chown mds:mds /data

COPY --from=build /out/microdevicestatus /usr/local/bin/microdevicestatus
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 755 /usr/local/bin/docker-entrypoint.sh /usr/local/bin/microdevicestatus

ENV MDS_ADDR=":8080" \
    MDS_DB_PATH="/data/micro-device-status.db"

EXPOSE 8080
VOLUME ["/data"]
WORKDIR /data

# Entrypoint starts as root only to fix volume ownership, then drops to mds.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/microdevicestatus"]
