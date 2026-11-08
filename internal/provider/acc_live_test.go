package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Live acceptance tests against a real ER605. They only touch disposable rows (disabled where the router allows it)
// and delete them again. Run with:
//
//	TF_ACC=1 ER605_ACC_URL=http://192.168.0.1 ER605_ACC_USERNAME=... ER605_ACC_PASSWORD=... go test ./internal/provider -run TestLive -v
//
// Without those variables the tests are skipped.
func liveConfig(t *testing.T, body string) string {
	t.Helper()
	url, user, pass := os.Getenv("ER605_ACC_URL"), os.Getenv("ER605_ACC_USERNAME"), os.Getenv("ER605_ACC_PASSWORD")
	if os.Getenv("TF_ACC") == "" || url == "" || user == "" || pass == "" {
		t.Skip("set TF_ACC, ER605_ACC_URL, ER605_ACC_USERNAME, ER605_ACC_PASSWORD to run against a router")
	}
	return fmt.Sprintf("provider \"er605\" {\n  url      = %q\n  username = %q\n  password = %q\n}\n", url, user, pass) + body
}

func TestLiveIPGroupAndPolicyChain(t *testing.T) {
	config := func(comment string) string {
		return liveConfig(t, fmt.Sprintf(`
resource "er605_ip_address" "a" {
  name       = "tfacc_net"
  type       = "mask"
  scope_mask = "10.99.99.0/24"
  comment    = %q
}
resource "er605_ip_group" "g" {
  name       = "tfacc_grp"
  rule_scope = [er605_ip_address.a.name]
}
`, comment))
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config("one"), Check: resource.TestCheckResourceAttr("er605_ip_group.g", "rule_scope.#", "1")},
			{Config: config("two"), Check: resource.TestCheckResourceAttr("er605_ip_address.a", "comment", "two")},
			{Config: config("two"), ResourceName: "er605_ip_group.g", ImportState: true, ImportStateId: "tfacc_grp", ImportStateVerify: true},
		},
	})
}

func TestLiveDisabledVirtualServer(t *testing.T) {
	config := func(port string) string {
		return liveConfig(t, fmt.Sprintf(`
resource "er605_virtual_server" "v" {
  name          = "tfacc_vs"
  interface     = "WAN1"
  external_port = "38443"
  internal_port = %q
  ipaddr        = "192.168.0.250"
  protocol      = "TCP"
  enable        = false
}
`, port))
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config("443"), Check: resource.TestCheckResourceAttr("er605_virtual_server.v", "enable", "false")},
			{Config: config("444"), Check: resource.TestCheckResourceAttr("er605_virtual_server.v", "internal_port", "444")},
		},
	})
}
