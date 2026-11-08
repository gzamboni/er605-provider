# SPEC - terraform-provider-er605: what is done and what is left

Target: TP-Link ER605 (standalone mode), firmware 1.3.1 Build 20231207, over its web API
(`/cgi-bin/luci/;stok=<token>/admin/<module>?form=<form>`). Protocol notes are in `README.md`, the ~200 forms in `docs/endpoints.md`.

## Done (verified on the live router)

Typed resources (table in `internal/provider/specs.go`, engine in `typed_resource.go`): `dhcp_server`, `time`, `management`, `nat_alg`,
`upnp`, `dos_defense`, `flood_defense`, `ip_mac_binding`, `mac_filter`, `dhcp_reservation`, `virtual_server`, `dmz`, `port_trigger`,
`static_route`, `mac_filter_entry`, `ip_address`, `ip_group`, `policy_route`, `lan`, `vlan`, `acl_rule`, `qos`, `qos_rule`, `qos_tag`.
Custom: `wan`, `vlan_port`, `admin_account`. Escape hatches: `er605_read`, `er605_setting`, `er605_list_item`.
All typed resources support `terraform import` (+ `-generate-config-out`).

## Not done / open

### 1. Screens with a known write shape, no resource yet
- **VPN users + IP pool** (`/admin/pppox?form=usermanager`, `/admin/ippool?form=pool_list`): add/set/delete verified by hand
  (add needs an existing pool; user body = username, password, type pptp|l2tp|open_vpn, localip, ippool, ms_dns1/2, netmode, maxsessions,
  remotesubnet; password is read back in clear). Only needs two `spec` entries + tests.
- **DDNS** (`dynddns`, `noipddns`, `phddns`, `cmxddns`, `dyn3322ddns`, `customddns`): fields in page JS (interface, name, passwd, domain, interval, enable). Secrets: mark `secret`.
- **Time objects** (`time_mngt?form=timeobj`), **service** (`service?form=service`), **reboot schedule**, **DNS proxy rules**, **DST** (`time?form=dst`),
  **SNMP**, **logger/syslog**, **IPv6** forms, **mDNS**, **session limits**, **URL/web filter**, **ARP/IP-MAC import**: reads work (see `docs/endpoints.md`);
  need field lists from the page JS and a live add/set/delete check each.

### 2. Screens where the write payload is still unknown
- **QoS classes** (`qos_ctl?form=class`), **per-WAN bandwidth** (`qos_ctl?form=bandwidth`), **VoIP** (`qos_ctl?form=voip`): `set` with the read shape
  fails with Lua errors. Capture the UI request (hook `$(document).ajaxSend` on the logged-in page, click Save) to get the real shape.
- **Authentication users** (`usermngr?form=usermngr`): `add` fails with `field 'day' missing in date table` - `expireTime`/`validPeriod` format unknown. Capture from the UI.
- **IPsec, OpenVPN, L2TP, PPTP servers/clients, LDAP profiles, portal/wportal, USB, firmware/backup/reset/reboot**: not investigated. Actions
  (reboot, upgrade, factory reset, config backup/restore) fit better as data sources / ephemeral actions than resources.

### 3. Gaps in what exists
- `wan`: `static` proto fields follow the UI but were never written; second (alias) connection (`second_proto`) not modelled; WAN mode / port mapping (`interface_wan?form=wanmode`) and MAC clone (`mac_setting`) missing.
- `lan`: creating an extra LAN untested; `bridge` mode untested; DHCP options (`dhcp_opts`) not modelled.
- `vlan_port`: only `general` + unchanged PVID verified; `access`/`trunk` rejected by the router on ports 4-5 (`error 80013`); PVID change untested (risk of cutting the session).
- `admin_account`: password is never returned, so no drift detection; failed logins count against the router lockout (9 attempts seen).
- `upnp`: the test left `external_iface` as `[""]` instead of `{}` (equivalent, UPnP off); clearing a list is written as `""`.
- List resources track rows by key, not by position: ordering for `acl_rule` / `qos_rule` (no readable position) is "append only"; `policy_route` has `position`.
- No retry/backoff beyond 3 login attempts; the router serialises requests and can take 5-35 s per write. No per-resource timeouts.
- Router error codes are surfaced as numbers; `chunk-common.*.js` contains the English message table (e.g. 36002 = rule conflicts) and could be embedded.

### 4. Quality / project
Done: unit tests (`encrypt` vector from `encrypt.js`, `tfValue`, `rowParams`, spec sanity), client tests and hermetic provider tests against
`internal/fakerouter` (lifecycle + import, singleton overlay, empty-list quirk, single-member list, policy-route position, admin account, per-form lock
regression), gated live tests (`TestLive*`), generated docs, CHANGELOG, CI workflow, goreleaser config, provider `timeout`, state/secret guidance.
Still open:
- Hermetic tests cover 6 resources; extend `fakerouter` quirks as new screens are added (it does not validate field values like the real router).
- Live tests cover ip_address/ip_group/virtual_server only; no live test for LAN/WAN/admin (deliberately: they can cut access).
- Release: no GPG signing, no registry publication, no version stamp in the binary.
- Retries are fixed (3 login attempts); no configurable retry/backoff.
- Router error codes are shown as numbers (message table in `chunk-common.*.js` could be embedded).
- Concurrency: one admin session at a time on the router; calls are serialised, so Terraform parallelism is effectively 1.

## Rules of thumb for the next screens
1. Find the page in `/webpages/pages/userrpm/<page>.html` (send `Referer: .../webpages/index.html`), read editor fields, `keyProperty`, `extraProperty`.
2. Do a live add/set/delete with a disposable row (disabled where possible), then add a `spec` entry.
3. Writes that matter (LAN/WAN/ports/admin) must be tested with a same-value or reversible change and the router compared to its prior read.
