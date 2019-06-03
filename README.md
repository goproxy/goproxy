# Goproxy

[![Go Report Card](https://goreportcard.com/badge/github.com/goproxy/goproxy)](https://goreportcard.com/report/github.com/goproxy/goproxy)
[![GoDoc](https://godoc.org/github.com/goproxy/goproxy?status.svg)](https://godoc.org/github.com/goproxy/goproxy)

A minimalist Go module proxy handler.

## Installation

Open your terminal and execute

```bash
$ go get github.com/goproxy/goproxy
```

done.

> The only requirement is the [Go](https://golang.org), at least v1.11.

## Usage

Create a file named `goproxy.go`

```go
package main

import (
	"net/http"

	"github.com/goproxy/goproxy"
)

func main() {
	http.ListenAndServe("localhost:8080", goproxy.New())
}
```

and run it

```bash
$ go run goproxy.go
```

then visit `http://localhost:8080`.

## Community

If you want to discuss Goproxy, or ask questions about it, simply post questions
or ideas [here](https://github.com/goproxy/goproxy/issues).

## Contributing

If you want to help build Goproxy, simply follow
[this](https://github.com/goproxy/goproxy/wiki/Contributing) to send pull
requests [here](https://github.com/goproxy/goproxy/pulls).

## License

This project is licensed under the Unlicense.

License can be found [here](LICENSE).
