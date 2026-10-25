// Command domain-cli is the terminal client for RegisterDomainCLI: domain
// search, registration, renewal, transfer, DNS and account management.
//
// The version is injected at release build time:
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/domain-cli
package main

import (
	"os"

	"github.com/registerdomaincli/cli/cli"
)

// version defaults to "dev" and is overridden by -ldflags -X main.version=.
var version = "dev"

func main() {
	if err := cli.NewRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}
