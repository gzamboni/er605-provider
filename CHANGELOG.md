# Changelog

## Unreleased

### Added
- Provider talking to the ER605 standalone web API (RSA login, one short session per call).
- Typed resources: `dhcp_server`, `time`, `management`, `nat_alg`, `upnp`, `dos_defense`, `flood_defense`, `ip_mac_binding`,
  `mac_filter`, `dhcp_reservation`, `virtual_server`, `dmz`, `port_trigger`, `static_route`, `mac_filter_entry`, `ip_address`,
  `ip_group`, `policy_route`, `lan`, `vlan`, `acl_rule`, `qos`, `qos_rule`, `qos_tag`, plus `wan`, `vlan_port`, `admin_account`.
- Escape hatches: `er605_read` data source, `er605_setting` and `er605_list_item` resources.
- `terraform import` for every typed resource (use with `-generate-config-out`).
- Provider `timeout` setting.
- Tests: client + engine unit tests, hermetic provider tests (real Terraform CLI against `internal/fakerouter`), gated live tests.
- Pre-commit hooks (hygiene, gitleaks, gofmt, vet, mod tidy; go test + docs freshness on push).
- Generated docs (`docs/`, `tfplugindocs`), CI workflow, goreleaser config.

### Fixed
- Concurrent creates/deletes on one list no longer act on stale row positions (per-form lock).
