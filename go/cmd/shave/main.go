package main

import (
	"os"

	"github.com/rahulkj/shave/internal/shave"
)

func main() {
	os.Exit(shave.Main(os.Args[1:]))
}
