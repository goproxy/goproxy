FROM golang:1.22-alpine3.19 AS build

WORKDIR /usr/local/src/goproxy
COPY . .

RUN set -eux; \
	if [ -d dist ]; then \
		GOARCH=$(go env GOARCH); \
		BIN_DIR=dist/goproxy_linux_$GOARCH; \
		[ $GOARCH == "amd64" ] && BIN_DIR=${BIN_DIR}_v1; \
		[ $GOARCH == "arm" ] && BIN_DIR=${BIN_DIR}_$(go env GOARM | cut -d , -f 1); \
		cp -r $BIN_DIR bin; \
	else \
		apk add --no-cache git; \
		go mod download; \
		CGO_ENABLED=0 go build \
			-trimpath \
			-ldflags "-s -w -X github.com/goproxy/goproxy/cmd/goproxy/internal.Version=$(git describe --dirty --tags --always)" \
			-o bin/ \
			./cmd/goproxy; \
	fi

FROM alpine:3.19

COPY --from=build /usr/local/src/goproxy/bin/ /usr/local/bin/

RUN apk add --no-cache go git git-lfs openssh gpg subversion fossil mercurial breezy
RUN git lfs install

USER nobody
WORKDIR /go
VOLUME /go
WORKDIR /goproxy
VOLUME /goproxy
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/goproxy"]
CMD ["server", "--address", ":8080"]
