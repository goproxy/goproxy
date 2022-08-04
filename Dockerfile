FROM golang:alpine AS build

COPY . /usr/local/src/goproxy

RUN apk add --no-cache git
RUN cd /usr/local/src/goproxy && go mod download && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/ ./cmd/goproxy

FROM alpine

COPY --from=build /usr/local/src/goproxy/bin/ /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/goproxy"]
