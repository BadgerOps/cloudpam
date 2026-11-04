package main

import (
	"reflect"
	"testing"

	"cloudpam/internal/domain"
)

func TestSingleAccountForEachProvider(t *testing.T) {
	aws := singleAccount(&Config{Provider: "aws", AccountID: 5, AWSRegions: []string{"us-east-1"}, GCPProject: "ignored"})
	if !reflect.DeepEqual(aws, domain.Account{ID: 5, Provider: "aws", Regions: []string{"us-east-1"}}) {
		t.Fatalf("aws account = %+v", aws)
	}
	gcp := singleAccount(&Config{Provider: "gcp", AccountID: 6, GCPProject: "my-project", GCPRegions: []string{"us-central1"}, AWSRegions: []string{"ignored"}})
	if !reflect.DeepEqual(gcp, domain.Account{ID: 6, Provider: "gcp", ExternalID: "my-project", Regions: []string{"us-central1"}}) {
		t.Fatalf("gcp account = %+v", gcp)
	}
}

func TestNewSingleAccountCollectorMatchesTheProvider(t *testing.T) {
	for _, provider := range []string{"aws", "gcp"} {
		c, err := newSingleAccountCollector(&Config{Provider: provider})
		if err != nil {
			t.Fatalf("%s: error = %v", provider, err)
		}
		if c.Provider() != provider {
			t.Fatalf("%s: collector provider = %q", provider, c.Provider())
		}
	}
	if _, err := newSingleAccountCollector(&Config{Provider: "azure"}); err == nil {
		t.Fatal("azure: want an error")
	}
}
