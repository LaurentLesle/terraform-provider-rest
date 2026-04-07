package main

import (
	"context"
	"flag"
	"log"

	"github.com/LaurentLesle/terraform-provider-rest/internal/provider"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

//go:generate go run ./internal/tools/gendoc

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	ctx := context.Background()
	serveOpts := providerserver.ServeOpts{
		Debug:   debug,
		Address: "registry.terraform.io/laurentlesle/rest",
	}

	err := providerserver.Serve(ctx, provider.New, serveOpts)

	if err != nil {
		log.Fatalf("Error serving provider: %s", err)
	}
}
