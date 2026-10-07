// Command agentcli is the provider-agnostic agent CLI dispatcher.
package main

import (
	"os"

	"github.com/JustAzul/agentcli/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args, os.Environ())) }
