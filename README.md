# Goproxy

[![Test](https://github.com/goproxy/goproxy/actions/workflows/test.yaml/badge.svg)](https://github.com/goproxy/goproxy/actions/workflows/test.yaml)
[![codecov](https://codecov.io/gh/goproxy/goproxy/branch/master/graph/badge.svg)](https://codecov.io/gh/goproxy/goproxy)
[![Go Report Card](https://goreportcard.com/badge/github.com/goproxy/goproxy)](https://goreportcard.com/report/github.com/goproxy/goproxy)
[![Go Reference](https://pkg.go.dev/badge/github.com/goproxy/goproxy.svg)](https://pkg.go.dev/github.com/goproxy/goproxy)

A minimalist Go module proxy handler.

Goproxy has fully implemented the [GOPROXY protocol](https://go.dev/ref/mod#goproxy-protocol). The goal of this project
is to find the most dead simple way to provide a minimalist handler that can act as a full-featured Go module proxy for
those who want to build their own proxies.

## Features

- Extremely easy to use
  - Three structs: [`goproxy.Goproxy`](https://pkg.go.dev/github.com/goproxy/goproxy#Goproxy),
    [`goproxy.GoFetcher`](https://pkg.go.dev/github.com/goproxy/goproxy#GoFetcher), and
    [`goproxy.DirCacher`](https://pkg.go.dev/github.com/goproxy/goproxy#DirCacher)
  - Two interfaces: [`goproxy.Fetcher`](https://pkg.go.dev/github.com/goproxy/goproxy#Fetcher) and
    [`goproxy.Cacher`](https://pkg.go.dev/github.com/goproxy/goproxy#Cacher)
- Built-in support for `GOPROXY`, `GONOPROXY`, `GOSUMDB`, `GONOSUMDB`, and `GOPRIVATE`
- Supports serving under other Go module proxies by setting `GOPROXY`
- Supports [proxying checksum databases](https://go.dev/design/25530-sumdb#proxying-a-checksum-database)
- Supports `Disable-Module-Fetch` header

## Installation

- To use this project programmatically, `go get` it:

  ```bash
  go get github.com/goproxy/goproxy
  ```

- To use this project from the command line, download the pre-built binaries from
  [here](https://github.com/goproxy/goproxy/releases) or build it from source:

  ```bash
  go install github.com/goproxy/goproxy/cmd/goproxy@latest
  ```

- To use this project with Docker, pull the pre-built images from
  [here](https://github.com/goproxy/goproxy/pkgs/container/goproxy):

  ```bash
  docker pull ghcr.io/goproxy/goproxy
  ```

## Quick Start

<details><summary>Write code</summary>

Create a file named `goproxy.go`:

```go
package main

import (
	"net/http"

	"github.com/goproxy/goproxy"
)

func main() {
	http.ListenAndServe("localhost:8080", &goproxy.Goproxy{})
}
```

Then run it with a `GOMODCACHE` that differs from `go env GOMODCACHE`:

```bash
GOMODCACHE=/tmp/goproxy-gomodcache go run goproxy.go
```

Finally, set `GOPROXY` to try it out:

```bash
go env -w GOPROXY=http://localhost:8080,direct
```

For more details, refer to the [documentation](https://pkg.go.dev/github.com/goproxy/goproxy).

</details>

<details><summary>Run from command line</summary>

Refer to the [Installation](#installation) section to download or build the binary.

Then run it with a `GOMODCACHE` that differs from `go env GOMODCACHE`:

```bash
GOMODCACHE=/tmp/goproxy-gomodcache goproxy server --address localhost:8080
```

Finally, set `GOPROXY` to try it out:

```bash
go env -w GOPROXY=http://localhost:8080,direct
```

For more details, check its usage:

```bash
goproxy --help
```

</details>

<details><summary>Run with Docker</summary>

Refer to the [Installation](#installation) section to pull the image.

Then run it:

```bash
docker run -p 8080:8080 ghcr.io/goproxy/goproxy server --address :8080
```

Finally, set `GOPROXY` to try it out:

```bash
go env -w GOPROXY=http://localhost:8080,direct
```

For more details, check its usage:

```bash
docker run ghcr.io/goproxy/goproxy --help
```

</details>

## Community

If you have any questions or ideas about this project, feel free to discuss them
[here](https://github.com/goproxy/goproxy/discussions).

## Contributing

If you would like to contribute to this project, please submit issues [here](https://github.com/goproxy/goproxy/issues)
or pull requests [here](https://github.com/goproxy/goproxy/pulls).

When submitting a pull request, please make sure its commit messages adhere to
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/).

## License

This project is licensed under the [MIT License](LICENSE).
