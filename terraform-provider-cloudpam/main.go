// terraform-provider-cloudpam is the Terraform provider plugin for CloudPAM.
//
// It manages CloudPAM IP address pools and cloud accounts through the CloudPAM
// REST API. Terraform launches this binary; running it directly is only useful
// with -debug for provider development.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/provider"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/BadgerOps/cloudpam",
		Debug:   debug,
	}

	if err := providerserver.Serve(context.Background(), provider.New(version), opts); err != nil {
		log.Fatal(err.Error())
	}
}
