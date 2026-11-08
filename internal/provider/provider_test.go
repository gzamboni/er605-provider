package provider

import (
	"fmt"
	"testing"

	"github.com/gzamboni/terraform-provider-er605/internal/fakerouter"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// These tests drive the real provider through the Terraform CLI against internal/fakerouter, so they need a
// `terraform` binary on PATH but no router. TF_ACC is not required (resource.UnitTest).

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"er605": providerserver.NewProtocol6WithError(New()),
}

func cfg(r *fakerouter.Router, body string) string {
	return fmt.Sprintf("provider \"er605\" {\n  url      = %q\n  username = \"admin\"\n  password = \"secret1\"\n}\n", r.URL) + body
}

func step(r *fakerouter.Router, body string, checks ...resource.TestCheckFunc) resource.TestStep {
	return resource.TestStep{Config: cfg(r, body), Check: resource.ComposeAggregateTestCheckFunc(checks...)}
}

func check(f func() error) resource.TestCheckFunc { return func(*terraform.State) error { return f() } }

func rowEquals(r *fakerouter.Router, endpoint string, i int, want map[string]any) error {
	rows := r.List(endpoint)
	if i >= len(rows) {
		return fmt.Errorf("%s has %d rows, want index %d", endpoint, len(rows), i)
	}
	for k, v := range want {
		if fmt.Sprint(rows[i][k]) != fmt.Sprint(v) {
			return fmt.Errorf("%s[%d].%s = %v, want %v (row %v)", endpoint, i, k, rows[i][k], v, rows[i])
		}
	}
	return nil
}

func TestListRowLifecycleAndImport(t *testing.T) {
	r := fakerouter.New(t, "admin", "secret1")
	const ep = "/admin/nat?form=vs"
	body := func(port string) string {
		return fmt.Sprintf(`resource "er605_virtual_server" "web" {
  name          = "web"
  interface     = "WAN1"
  external_port = "8443"
  internal_port = %q
  ipaddr        = "192.168.0.10"
  protocol      = "TCP"
}`, port)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             func(*terraform.State) error { return nil },
		Steps: []resource.TestStep{
			step(r, body("443"),
				resource.TestCheckResourceAttr("er605_virtual_server.web", "id", "web"),
				resource.TestCheckResourceAttr("er605_virtual_server.web", "enable", "true"),
				check(func() error {
					return rowEquals(r, ep, 0, map[string]any{"name": "web", "internal_port": "443", "enable": "on"})
				})),
			step(r, body("8443"),
				check(func() error { return rowEquals(r, ep, 0, map[string]any{"internal_port": "8443"}) })),
			{Config: cfg(r, body("8443")), ResourceName: "er605_virtual_server.web", ImportState: true, ImportStateId: "web", ImportStateVerify: true},
		},
	})
	if n := len(r.List(ep)); n != 0 {
		t.Fatalf("destroy left %d rows", n)
	}
	if r.Sessions() != 0 {
		t.Fatalf("%d sessions left open", r.Sessions())
	}
}

func TestSingletonOverlaysOnCurrentValuesAndKeepsUnmanagedKeys(t *testing.T) {
	r := fakerouter.New(t, "admin", "secret1")
	const ep = "/admin/dhcps?form=setting"
	r.SeedSingle(ep, map[string]any{"enable": "on", "leasetime": "120", "domain": "lab", "dhcp_opts": map[string]any{}, "maxleases": 1024})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			step(r, `resource "er605_dhcp_server" "d" {
  leasetime = 300
  enable    = false
}`,
				resource.TestCheckResourceAttr("er605_dhcp_server.d", "leasetime", "300"),
				resource.TestCheckResourceAttr("er605_dhcp_server.d", "domain", "lab"), // untouched attribute read back
				check(func() error {
					got := r.Single(ep)
					if got["leasetime"] != "300" || got["enable"] != "off" || got["domain"] != "lab" || got["maxleases"] != float64(1024) {
						return fmt.Errorf("router state = %v", got)
					}
					return nil
				})),
		},
	})
}

func TestEmptyListIsSentAsEmptyString(t *testing.T) {
	r := fakerouter.New(t, "admin", "secret1")
	const ep = "/admin/upnp?form=upnp_global"
	r.SeedSingle(ep, map[string]any{"state": "off", "server_iface": []any{"LAN1"}, "external_iface": map[string]any{}})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			step(r, `resource "er605_upnp" "u" {
  state          = false
  server_iface   = ["LAN1"]
  external_iface = []
}`,
				resource.TestCheckResourceAttr("er605_upnp.u", "external_iface.#", "0"),
				check(func() error {
					if v := r.Single(ep)["external_iface"]; v != "" {
						return fmt.Errorf("external_iface sent as %#v, want empty string", v)
					}
					return nil
				})),
		},
	})
}

func TestSingleMemberListReadFromBareString(t *testing.T) {
	r := fakerouter.New(t, "admin", "secret1")
	const ep = "/admin/ipgroup?form=ipgroup_reservation"
	// the router returns a bare string when a group has one member
	r.SeedList(ep, map[string]any{"name": "IPGROUP_LAN", "rule_scope": "IP_LAN", "comment": "x"})
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: cfg(r, `import {
  to = er605_ip_group.lan
  id = "IPGROUP_LAN"
}
resource "er605_ip_group" "lan" {
  name       = "IPGROUP_LAN"
  rule_scope = ["IP_LAN"]
  comment    = "x"
}`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("er605_ip_group.lan", "rule_scope.0", "IP_LAN"),
					resource.TestCheckResourceAttr("er605_ip_group.lan", "rule_scope.#", "1")),
			},
		},
	})
}

func TestPolicyRoutePositionDefaultsToLast(t *testing.T) {
	r := fakerouter.New(t, "admin", "secret1")
	const ep = "/admin/policy_route?form=policy_route"
	rule := func(name string) string {
		return fmt.Sprintf(`resource "er605_policy_route" %q {
  name         = %q
  service_type = "ALL"
  src_ipgroup  = "A"
  dst_ipgroup  = "B"
  interfaces   = "WAN1"
  timeobj      = "Any"
  mode         = "Priority"
}
`, name, name)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			step(r, rule("one")+rule("two"),
				check(func() error {
					rows := r.List(ep)
					if len(rows) != 2 {
						return fmt.Errorf("want 2 rows, got %v", rows)
					}
					seen := map[string]bool{fmt.Sprint(rows[0]["index"]): true, fmt.Sprint(rows[1]["index"]): true}
					if !seen["1"] || !seen["2"] {
						return fmt.Errorf("positions = %v", seen)
					}
					return nil
				})),
		},
	})
}

func TestAdminAccountChangesCredentials(t *testing.T) {
	r := fakerouter.New(t, "admin", "secret1")
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			step(r, `resource "er605_admin_account" "a" {
  username = "root"
  password = "newsecret"
}`,
				check(func() error {
					if r.Account["old_pwd"] != "secret1" || r.Account["new_acc"] != "root" || r.Account["new_pwd"] != "newsecret" {
						return fmt.Errorf("account change = %v", r.Account)
					}
					return nil
				})),
		},
	})
}
