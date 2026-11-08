# terraform-provider-er605

Terraform provider for the TP-Link ER605 in standalone mode. Built by reverse-engineering the router's web UI
(`/cgi-bin/luci/;stok=<token>/admin/<module>?form=<form>`, raw-RSA password login). See `docs/endpoints.md` for the
~200 forms the UI exposes.

## Build / use

```sh
go build -o terraform-provider-er605 .
# ~/.terraformrc
provider_installation {
  dev_overrides { "registry.terraform.io/gzamboni/er605" = "/path/to/er605-provider" }
  direct {}
}
export ER605_USERNAME=... ER605_PASSWORD=...   # ER605_URL defaults to http://192.168.0.1
```

The router's HTTPS only speaks legacy TLS (modern Go/OpenSSL fail the handshake), so use `http://` on the LAN.
The provider opens a short login session per API call and logs out, so the web UI is never locked out.

## Typed resources (one per web-UI screen)

Defined in `internal/provider/specs.go` (a table; the engine is `typed_resource.go`).

| resource | screen | kind |
|---|---|---|
| `er605_wan` (`wan_id` + `proto`) | Network > WAN connection per port (dhcp / static / pppoe; import with the wan_id) | singleton per port |
| `er605_lan` (key `name`) | Network > LAN network list (ip/netmask/vlan) | row |
| `er605_admin_account` | System > Administration > Account (management login) | singleton, write-only |
| `er605_vlan` (key `vlan`) | Network > VLAN list | row |
| `er605_vlan_port` (key `port`) | Network > VLAN > port type/PVID | per port |
| `er605_acl_rule` (key `name`) | Firewall > Access control | row |
| `er605_qos`, `er605_qos_tag` | Transmission > QoS global / tagging switches | singleton |
| `er605_qos_rule` (key `name`) | QoS bandwidth control rules | row |
| `er605_dhcp_server` | LAN > DHCP server | singleton |
| `er605_time` | System > Time | singleton |
| `er605_management` | System > Administration ports/timeout | singleton |
| `er605_nat_alg` | NAT > ALG | singleton |
| `er605_upnp` | NAT > UPnP global | singleton |
| `er605_dos_defense`, `er605_flood_defense` | Firewall > Attack defense | singleton |
| `er605_ip_mac_binding` | IP-MAC binding global | singleton |
| `er605_mac_filter` | MAC filtering mode | singleton |
| `er605_dhcp_reservation` (key `mac`) | LAN > DHCP static reservations | row |
| `er605_virtual_server` (key `name`) | NAT > Virtual servers | row |
| `er605_dmz`, `er605_port_trigger`, `er605_static_route` (key `name`) | NAT / Routing | row |
| `er605_mac_filter_entry` (key `mac`) | MAC filtering list | row |
| `er605_policy_route` (key `name`) | Routing > Policy routing (ordered; `position` defaults to last) | row |
| `er605_ip_address`, `er605_ip_group` (key `name`) | Preferences > IP groups (address entries; groups reference them by name) | row |

Singletons always exist: `apply` sets only the attributes you declare (rest keep the router's values), `destroy` just stops managing them.
Rows are located by their key attribute on every operation (the router addresses rows by position).

### Reading the current configuration into Terraform

Every typed resource supports import, so you can pull the router's config into HCL:

```hcl
import {
  to = er605_dhcp_server.main
  id = "x"                    # singletons: any id
}
import {
  to = er605_dhcp_reservation.printer
  id = "AA-BB-CC-DD-EE-01"   # rows: the key value
}
```
`terraform plan -generate-config-out=generated.tf` writes the resource blocks.

```hcl
resource "er605_virtual_server" "web" {
  name = "web"; interface = "WAN1"; external_port = "8443"; internal_port = "443"
  ipaddr = "192.168.0.10"; protocol = "TCP"
}
```

## Escape hatches (raw API, any of the ~200 forms in docs/endpoints.md)

* `data "er605_read"` - read any form: `jsondecode(data.er605_read.x.json)`.
* `resource "er605_setting"` / `"er605_list_item"` - JSON-bodied singleton / row for screens without a typed resource.

## Adding a typed resource

Append a `spec` to `specs.go`: endpoint, key (lists), and fields. Look up field names in the page JS
(`/webpages/pages/userrpm/<page>.html`, send header `Referer: .../webpages/index.html`) and verify with a live get/add/set/delete.
Quirks found so far: list set/add use the names the grid reads; some grids key rows on a field (`rowKey`, `rowExtra`);
an empty list is written as `""` (the router rejects `[]`); writes can take 5-35 s because the router applies each change.

LAN/WAN notes: LAN set must carry the grid key property (`vlanid`) and `[]` for empty `dhcp_opts`; the default LAN is never deleted.
WAN: verified on dhcp (WAN2/3 import, WAN3 write) and pppoe (WAN1 write); `static` fields follow the UI but are untested. Each WAN write re-dials the link (10-20 s).
To set an empty string on an Optional attribute use `""`, not `null` (null means "keep the router's value").

Admin account: `er605_admin_account` replaces the single management login using the provider's *current* credentials, then the
provider switches to the new ones for the rest of the run. Update `username`/`password` in the provider config (or `ER605_*` env)
before the next run. The router never returns the password, so there is no drift detection. Failed logins are counted by the
router (9 attempts allowed in a test), so a wrong provider password can lock you out.

VLAN/ACL/QoS notes: `er605_vlan_port` writes the whole port table (the router only accepts that); on the ER605 `access`/`trunk` were refused
(error 80013) on ports 4-5, so only `general` + PVID is verified, and only with unchanged values. ACL `position` and QoS-rule `position` are not exposed:
new rules go last. QoS rules need `er605_qos` saved once (the router's `qos_setting_number` is created by that save). Not covered: QoS class rules,
per-WAN bandwidth classes and VoIP (`qos_ctl`; their write payloads were not found).

## Development

```sh
go test ./... -race          # unit tests + hermetic provider tests (needs the terraform CLI, no router)
go generate ./...            # regenerate docs/ with tfplugindocs
TF_ACC=1 ER605_ACC_URL=http://192.168.0.1 ER605_ACC_USERNAME=... ER605_ACC_PASSWORD=... \
  go test ./internal/provider -run TestLive -v     # live tests: disposable rows only, deleted afterwards
```

`internal/fakerouter` is an in-memory ER605 (RSA login, singleton and list forms, account form) used by the client tests and by the
provider tests, which run the real provider through the Terraform CLI. CI (`.github/workflows/ci.yml`) runs gofmt, vet, build, tests with `-race`
and checks that `docs/` is up to date. `.goreleaser.yml` builds release archives; add a GPG `signs:` block before publishing to the registry.

## State and secrets

The router returns PPPoE and other passwords in clear, so they end up in the Terraform state (marked sensitive, but stored). Keep the state in an
encrypted backend (or use OpenTofu state encryption) and restrict access to it. The provider password can come from `ER605_PASSWORD` instead of the config.

## Pre-commit

```sh
pre-commit install                      # installs the pre-commit and pre-push hooks (see .pre-commit-config.yaml)
pre-commit run --all-files              # file hygiene, gitleaks, gofmt, go vet, go mod tidy
pre-commit run --hook-stage pre-push --all-files   # adds go test and the docs/ freshness check
```
gitleaks scans every commit (rules + allowlist in `.gitleaks.toml`; test fixtures use throwaway fake-router credentials). Never put the router password in the repo: use `ER605_PASSWORD`.
