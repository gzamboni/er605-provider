package main

//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.20.1 generate --provider-name er605

import (
	"context"
	"log"

	"github.com/gzamboni/terraform-provider-er605/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/gzamboni/er605",
	})
	if err != nil {
		log.Fatal(err)
	}
}
