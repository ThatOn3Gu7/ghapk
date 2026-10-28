// Command ghapk finds the newest GitHub Actions build of your app and installs
// it on your phone. Run it with no arguments for the interactive interface.
package main

import (
	"os"

	"ghapk/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:])) }
