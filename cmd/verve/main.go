// Command verve is the Verve server and command line.
package main

import (
	"context"
	"os"

	// The timezones of the users load even on a system without a timezone database, such as
	// Windows. It adds about 400 KB to the binary.
	_ "time/tzdata"

	"github.com/gauthier-se/verve/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
