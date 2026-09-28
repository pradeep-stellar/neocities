package main

import (
	"os"

	"github.com/neocities/neocities"
)

func main() {
	os.Exit(neocities.Run(os.Args[1:]))
}
