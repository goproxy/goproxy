FROM golang:1.21-alpine3.18 AS build

WORKDIR /usr/local/src/goproxy
COPY . .

RUN apk add --no-cache git
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/ ./cmd/goproxy

FROM alpine:3.18

COPY --from=build /usr/local/src/goproxy/bin/ /usr/local/bin/

RUN apk add --no-cache go git git-lfs openssh gpg subversion fossil mercurial breezy
RUN git lfs install

ENTRYPOINT ["/usr/local/bin/goproxy"]
