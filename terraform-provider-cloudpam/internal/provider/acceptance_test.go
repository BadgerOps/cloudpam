package provider

// Acceptance tests. These drive a real `terraform` binary against a live
// CloudPAM server and are skipped unless TF_ACC is set, so they never run in
// the default `go test ./...` pass (including CI).
//
// To run them:
//
//	export TF_ACC=1
//	export CLOUDPAM_ENDPOINT=http://localhost:8080
//	export CLOUDPAM_API_KEY=cpam_...
//	go test ./internal/provider/ -run TestAcc -v

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccProtoV6ProviderFactories wires the in-process provider into the test
// harness so no provider binary has to be installed.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"cloudpam": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, key := range []string{EnvEndpoint, EnvAPIKey} {
		if os.Getenv(key) == "" {
			t.Fatalf("%s must be set for acceptance tests", key)
		}
	}
}

// acctestAccountKey builds a unique, schema-valid account key. CloudPAM
// validates `aws:` keys against a 12-digit account ID, so the uniqueness suffix
// has to stay numeric. n distinguishes the accounts used by different tests.
func acctestAccountKey(n int) string {
	return fmt.Sprintf("aws:%012d", os.Getpid()*10+n)
}

func TestAccAccountResource(t *testing.T) {
	key := acctestAccountKey(1)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cloudpam_account" "test" {
  key            = %q
  name           = "tfacc-account"
  cloud_provider = "aws"
  environment    = "staging"
  regions        = ["us-east-1"]
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudpam_account.test", "key", key),
					resource.TestCheckResourceAttr("cloudpam_account.test", "name", "tfacc-account"),
					resource.TestCheckResourceAttr("cloudpam_account.test", "cloud_provider", "aws"),
					resource.TestCheckResourceAttr("cloudpam_account.test", "regions.#", "1"),
					resource.TestCheckResourceAttrSet("cloudpam_account.test", "id"),
				),
			},
			{
				ResourceName:            "cloudpam_account.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"force_destroy"},
			},
			{
				// Update in place: rename and clear the optional fields.
				Config: fmt.Sprintf(`
resource "cloudpam_account" "test" {
  key  = %q
  name = "tfacc-account-renamed"
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudpam_account.test", "name", "tfacc-account-renamed"),
					resource.TestCheckResourceAttr("cloudpam_account.test", "cloud_provider", ""),
					resource.TestCheckResourceAttr("cloudpam_account.test", "regions.#", "0"),
				),
			},
		},
	})
}

func TestAccPoolResource(t *testing.T) {
	key := acctestAccountKey(2)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cloudpam_account" "test" {
  key  = %[1]q
  name = "tfacc-pool-account"
}

resource "cloudpam_pool" "parent" {
  name = "tfacc-parent"
  cidr = "10.240.0.0/16"
  type = "supernet"
}

resource "cloudpam_pool" "child" {
  name       = "tfacc-child"
  cidr       = "10.240.1.0/24"
  parent_id  = cloudpam_pool.parent.id
  account_id = cloudpam_account.test.id
  tags = {
    managed_by = "terraform"
  }
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudpam_pool.parent", "cidr", "10.240.0.0/16"),
					resource.TestCheckResourceAttr("cloudpam_pool.parent", "type", "supernet"),
					resource.TestCheckNoResourceAttr("cloudpam_pool.parent", "parent_id"),
					resource.TestCheckResourceAttrPair("cloudpam_pool.child", "parent_id", "cloudpam_pool.parent", "id"),
					resource.TestCheckResourceAttrPair("cloudpam_pool.child", "account_id", "cloudpam_account.test", "id"),
					resource.TestCheckResourceAttr("cloudpam_pool.child", "tags.managed_by", "terraform"),
				),
			},
			{
				ResourceName:            "cloudpam_pool.child",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"force_destroy"},
			},
			{
				// Dropping account_id must clear the assignment in place rather
				// than silently keeping the old account.
				Config: fmt.Sprintf(`
resource "cloudpam_account" "test" {
  key  = %[1]q
  name = "tfacc-pool-account"
}

resource "cloudpam_pool" "parent" {
  name = "tfacc-parent"
  cidr = "10.240.0.0/16"
  type = "supernet"
}

resource "cloudpam_pool" "child" {
  name      = "tfacc-child-renamed"
  cidr      = "10.240.1.0/24"
  parent_id = cloudpam_pool.parent.id
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("cloudpam_pool.child", "name", "tfacc-child-renamed"),
					resource.TestCheckNoResourceAttr("cloudpam_pool.child", "account_id"),
					resource.TestCheckResourceAttr("cloudpam_pool.child", "tags.%", "0"),
				),
			},
		},
	})
}

func TestAccPoolDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudpam_pool" "test" {
  name = "tfacc-ds"
  cidr = "10.241.0.0/16"
}

data "cloudpam_pool" "by_id" {
  id = cloudpam_pool.test.id
}

data "cloudpam_pool" "by_cidr" {
  cidr = cloudpam_pool.test.cidr
}

data "cloudpam_pools" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudpam_pool.by_id", "name", "tfacc-ds"),
					resource.TestCheckResourceAttr("data.cloudpam_pool.by_cidr", "name", "tfacc-ds"),
					resource.TestCheckResourceAttrPair("data.cloudpam_pool.by_cidr", "id", "cloudpam_pool.test", "id"),
					resource.TestCheckResourceAttrSet("data.cloudpam_pools.all", "pools.#"),
				),
			},
		},
	})
}

func TestAccAccountDataSource(t *testing.T) {
	key := acctestAccountKey(3)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cloudpam_account" "test" {
  key            = %[1]q
  name           = "tfacc-ds-account"
  cloud_provider = "aws"
}

data "cloudpam_account" "by_key" {
  key = cloudpam_account.test.key
}

data "cloudpam_accounts" "aws" {
  cloud_provider = "aws"
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudpam_account.by_key", "name", "tfacc-ds-account"),
					resource.TestCheckResourceAttrPair("data.cloudpam_account.by_key", "id", "cloudpam_account.test", "id"),
					resource.TestCheckResourceAttrSet("data.cloudpam_accounts.aws", "accounts.#"),
				),
			},
		},
	})
}
