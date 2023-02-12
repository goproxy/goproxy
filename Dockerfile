FROM golang:1.20-alpine3.17 AS build

COPY . /usr/local/src/goproxy

RUN apk add --no-cache git
RUN cd /usr/local/src/goproxy && go mod download && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/ ./cmd/goproxy

FROM alpine:3.17

COPY --from=build /usr/local/src/goproxy/bin/ /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/goproxy"]
