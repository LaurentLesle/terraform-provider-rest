package main

import (
	"context"
	"log"

	tffwdocs "github.com/magodo/terraform-plugin-framework-docs"
	"github.com/laurentlesle/terraform-provider-rest/internal/provider"
)

func main() {
	ctx := context.Background()
	gen, err := tffwdocs.NewGenerator(ctx, &provider.Provider{})
	if err != nil {
		log.Fatal(err)
	}

	if err := gen.Lint(nil); err != nil {
		log.Fatal(err)
	}

	if err := gen.WriteAll(ctx, "./docs", nil); err != nil {
		log.Fatal(err)
	}
}
