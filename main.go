package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/viettelcloud-oss/terraform-provider-viettelcloud/internal/provider"
)

// version is set at build time through ldflags.
var version string = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	address := os.Getenv("VIETTELCLOUD_PROVIDER_ADDRESS")

	opts := providerserver.ServeOpts{
		Address: address,
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
