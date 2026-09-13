ARG GO_BASE_IMAGE=golang:1.27-alpine3.24

FROM ${GO_BASE_IMAGE} AS build

ARG TARGETPLATFORM
ARG USE_GORELEASER_ARTIFACTS=0

WORKDIR /usr/local/src/goproxy
COPY . .

RUN <<'EOF'
set -eux

mkdir -p bin

if [ "${USE_GORELEASER_ARTIFACTS}" -eq 1 ]; then
	cp -p "${TARGETPLATFORM}/bin/goproxy" bin/
else
	apk add --no-cache git
	go mod download
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/ ./cmd/goproxy
fi
EOF

FROM ${GO_BASE_IMAGE}

COPY --from=build /usr/local/src/goproxy/bin/ /usr/local/bin/

RUN apk add --no-cache git git-lfs openssh gpg subversion fossil mercurial
RUN git lfs install --system

WORKDIR /goproxy
VOLUME /goproxy
EXPOSE 8080

USER nobody
ENTRYPOINT ["/usr/local/bin/goproxy"]
CMD ["server", "--address", ":8080"]
