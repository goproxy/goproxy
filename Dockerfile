FROM golang:1.23-alpine3.21 AS build

ARG USE_GORELEASER_ARTIFACTS=0
ARG GORELEASER_ARTIFACTS_TARBALL

WORKDIR /usr/local/src/goproxy
COPY . .

RUN set -eux; \
	if [ "${USE_GORELEASER_ARTIFACTS}" -eq 1 ]; then \
		tar -xzf "${GORELEASER_ARTIFACTS_TARBALL}" bin/goproxy; \
	else \
		apk add --no-cache git; \
		go mod download; \
		CGO_ENABLED=0 go build \
			-trimpath \
			-ldflags "-s -w -X github.com/goproxy/goproxy/cmd/goproxy/internal.Version=$(git describe --dirty --tags --always)" \
			-o bin/ \
			./cmd/goproxy; \
	fi

FROM alpine:3.21

COPY --from=build /usr/local/src/goproxy/bin/ /usr/local/bin/

RUN apk add --no-cache go git git-lfs openssh gpg subversion fossil mercurial breezy
RUN git lfs install --system

USER nobody
WORKDIR /go
VOLUME /go
WORKDIR /goproxy
VOLUME /goproxy
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/goproxy"]
CMD ["server", "--address", ":8080"]
