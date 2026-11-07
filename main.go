// Command gitopsify-provider-template is a gitopsify provider: it reads a
// tool's live state so "gitopsify export template" can write it as Terraform
// or Crossplane configuration.
package main

import (
	"github.com/hayone1/gitopsify-provider-template/internal/provider"
	"github.com/hayone1/gitopsify/sdk"
)

// version is set at build time (-ldflags "-X main.version=v0.1.0").
var version = "dev"

func main() { sdk.Serve(provider.New(version)) }
