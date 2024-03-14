FROM golang:1.22-alpine3.19 AS build

WORKDIR /usr/local/src/goproxy
COPY . .

RUN set -eux; \
	mkdir bin; \
	if [ -d dist ]; then \
		GOOS=$(go env GOOS); \
		GOARCH=$(go env GOARCH); \
		GOARM=$(go env GOARM); \
		BUILD_DIR=dist/goproxy_${GOOS}_${GOARCH}; \
		[ $GOARCH == "amd64" ] && BUILD_DIR=${BUILD_DIR}_v1; \
		[ $GOARCH == "arm" ] && BUILD_DIR=${BUILD_DIR}_$(echo $GOARM | cut -c 1); \
		cp $BUILD_DIR/goproxy bin/goproxy; \
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
