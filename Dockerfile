FROM golang:1.18 as builder

WORKDIR /src/goproxy

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o bin/goproxy ./cmd/goproxy

FROM alpine:3.16

WORKDIR /opt/goproxy

COPY --from=builder /src/goproxy/bin/goproxy bin/goproxy

ENV \
    ADDRESS=0.0.0.0:8080 \
    TLS_CERT_FILE= \
    TLS_KEY_FILE= \
    GO_BIN_NAME= \
    GO_BIN_MAX_WORKERS= \
    PATH_PREFIX= \
    CACHER_DIR=/var/cache/goproxy \
    CACHER_MAX_CACHE_BYTES= \
    PROXIED_SUMDBS= \
    TEMP_DIR= \
    INSECURE=

EXPOSE 8080

VOLUME ["/var/cache/goproxy"]

CMD ["sh", "-c", \
    "bin/goproxy \
    ${ADDRESS:+-address \"${ADDRESS}\"} \
    ${TLS_CERT_FILE:+-tls-cert-file \"${TLS_CERT_FILE}\"} \
    ${TLS_KEY_FILE:+-tls-key-file \"${TLS_KEY_FILE}\"} \
    ${GO_BIN_NAME:+-go-bin-name \"${GO_BIN_NAME}\"} \
    ${GO_BIN_MAX_WORKERS:+-go-bin-max-workers \"${GO_BIN_MAX_WORKERS}\"} \
    ${PATH_PREFIX:+-path-prefix \"${PATH_PREFIX}\"} \
    ${CACHER_DIR:+-cacher-dir \"${CACHER_DIR}\"} \
    ${CACHER_MAX_CACHE_BYTES:+-cacher-max-cache-bytes \"${CACHER_MAX_CACHE_BYTES}\"} \
    ${PROXIED_SUMDBS:+-proxied-sumdbs \"${PROXIED_SUMDBS}\"} \
    ${TEMP_DIR:+-temp-dir \"${TEMP_DIR}\"} \
    ${INSECURE:+-insecure} \
    "]
