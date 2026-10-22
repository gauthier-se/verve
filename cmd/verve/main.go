// Command verve is the Verve server and command line.
package main

import (
	"context"
	"os"

	"github.com/gauthier-se/verve/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
