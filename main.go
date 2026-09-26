// terraform-provider-webbpulse serves the WebbPulse Terraform control plane
// provider over the Terraform plugin protocol.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

// version is stamped at build time with -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/WebbPulse/webbpulse",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
