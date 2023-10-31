package main

import (
	"os"

	"github.com/goproxy/goproxy/cmd/goproxy/internal"
)

func main() { os.Exit(internal.Execute()) }
