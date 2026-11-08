package provider

// One spec per web-UI screen. Names are the Terraform attribute names; read()/wire() map them to the router's
// names where those differ. Field lists come from the router's own page JS and live reads.

func str(n string) field                { return field{name: n, k: kStr} }
func flag(n string) field               { return field{name: n, k: kBool} }
func num(n string) field                { return field{name: n, k: kInt} }
func strs(n string) field               { return field{name: n, k: kStrList} }
func (f field) required() field         { f.req = true; return f }
func (f field) rd(n string) field       { f.read = n; return f }
func (f field) wr(n string) field       { f.wire = n; return f }
func (f field) dflt(v bool) field       { f.def = &v; return f }
func (f field) about(s string) field    { f.desc = s; return f }
func (f field) words(t, o string) field { f.bools = [2]string{t, o}; return f }

func flags(names ...string) []field {
	out := make([]field, len(names))
	for i, n := range names {
		out[i] = flag(n)
	}
	return out
}

var specs = []*spec{
	// --- singleton screens ---
	{typeName: "dhcp_server", desc: "Network > LAN > DHCP server settings.", endpoint: "/admin/dhcps?form=setting", fields: []field{
		flag("enable"), str("ipaddr_start"), str("ipaddr_end"), num("leasetime").about("minutes"),
		str("gateway"), str("pri_dns"), str("snd_dns"), str("domain"), flag("relay_enable"), str("dhcp_mode"),
	}},
	{typeName: "time", desc: "System > Time settings (clock fields are read-only on the router and not managed).", endpoint: "/admin/time?form=settings",
		drop: []string{"day", "date", "time", "run"}, fields: []field{
			str("type").about("auto (NTP) or manual"), str("timezone").about("router timezone code, as in the web UI value"), str("ntp_svr1"), str("ntp_svr2"),
		}},
	{typeName: "nat_alg", desc: "Transmission > NAT > ALG.", endpoint: "/admin/nat?form=alg", fields: flags("ftp", "tftp", "h323", "sip", "pptp", "l2tp", "ipsec")},
	{typeName: "dos_defense", desc: "Firewall > Attack defense > DoS defense.", endpoint: "/admin/dos_defense?form=settings", fields: flags(
		"tcp_winnuke", "tcp_noflag", "tcp_fin_syn", "tcp_fin_noack", "tcp_scan_reject", "ping_death", "ping_wan", "ping_large",
		"ip_frag", "ip_option", "ipopt_timestamp", "ipopt_stream", "ipopt_noop", "ipopt_record_route", "ipopt_loose_route", "ipopt_strict_route", "ipopt_secure")},
	{typeName: "flood_defense", desc: "Firewall > Attack defense > Flood defense.", endpoint: "/admin/flood_defense?form=settings", fields: []field{
		flag("tcp_conn_en"), num("tcp_conn_lim"), flag("tcp_src_en"), num("tcp_src_lim"),
		flag("udp_conn_en"), num("udp_conn_lim"), flag("udp_src_en"), num("udp_src_lim"),
		flag("icmp_conn_en"), num("icmp_conn_lim"), flag("icmp_src_en"), num("icmp_src_lim"),
	}},
	{typeName: "management", desc: "System > Administration > management ports and timeout.", endpoint: "/admin/administration?form=management",
		drop: []string{"port_list"}, fields: []field{
			num("web_timeout").about("minutes"), flag("enable_https"), flag("redirect_https"), num("http_port"), num("https_port"),
		}},
	{typeName: "upnp", desc: "Transmission > NAT > UPnP global settings.", endpoint: "/admin/upnp?form=upnp_global", fields: []field{
		flag("state"), strs("server_iface"), strs("external_iface"),
	}},
	{typeName: "ip_mac_binding", desc: "Preferences > IP-MAC binding global settings.", endpoint: "/admin/imb?form=enable", fields: []field{
		flag("enable"), flag("garp"), flag("imb_pass"), flag("log_enable"), num("send_interval"), strs("interface"),
	}},

	// --- list screens: one resource per row, keyed by the field named in key ---
	{typeName: "dhcp_reservation", desc: "Network > LAN > DHCP static address reservation (keyed by MAC, format AA-BB-CC-DD-EE-FF).",
		endpoint: "/admin/dhcps?form=reservation", key: "mac", rowKey: "ip", rowExtra: "interface", fields: []field{
			str("mac"), str("ip").required(), str("interface").required().about("e.g. LAN1"),
			str("note"), flag("enable").dflt(true), flag("bind").words("1", "0").dflt(false).about("also create the IP-MAC binding"),
		}},
	{typeName: "virtual_server", desc: "Transmission > NAT > Virtual servers (port forwarding).", endpoint: "/admin/nat?form=vs", key: "name", fields: []field{
		str("name"), str("interface").required().about("e.g. WAN1"), str("external_port").required().about("port or range a-b"),
		str("internal_port").required(), str("ipaddr").required(), str("protocol").required().about("TCP, UDP or ALL"), flag("enable").dflt(true),
	}},
	{typeName: "dmz", desc: "Transmission > NAT > DMZ.", endpoint: "/admin/nat?form=dmz", key: "name", fields: []field{
		str("name"), str("interface").required(), str("ipaddr").required(), flag("enable").dflt(true),
	}},
	{typeName: "port_trigger", desc: "Transmission > NAT > Port triggering.", endpoint: "/admin/nat?form=pt", key: "name", fields: []field{
		str("name"), str("interface").required(), str("trigger_port").required(), str("trigger_protocol").required(),
		str("external_port").required(), str("external_protocol").required(), flag("enable").dflt(true),
	}},
	{typeName: "static_route", desc: "Transmission > Routing > Static routing.", endpoint: "/admin/routing?form=static_routing", key: "name", fields: []field{
		str("name"), str("target").required(), str("netmask").required(), str("gateway").required(), str("interface").required(),
		num("metric"), str("note"), flag("enable").dflt(true),
	}},
	{typeName: "mac_filter_entry", desc: "Firewall > MAC filtering list (keyed by MAC).", endpoint: "/admin/mac_filter?form=mac_list", key: "mac", fields: []field{
		str("mac"), str("name"),
	}},
	{typeName: "ip_address", desc: "Preferences > IP groups > IP address entry. Use scope for type=range (\"a.b.c.d-e.f.g.h\") or scope_mask for type=mask (\"a.b.c.d/nn\").",
		endpoint: "/admin/ipgroup?form=ipscope_reservation", key: "name", fields: []field{
			str("name"), str("type").required().about("range or mask"), str("scope"), str("scope_mask"), str("comment"),
		}},
	{typeName: "ip_group", desc: "Preferences > IP groups > group of ip_address entries (members referenced by name).",
		endpoint: "/admin/ipgroup?form=ipgroup_reservation", key: "name", fields: []field{
			str("name"), strs("rule_scope").required().about("names of member ip_address entries"), str("comment"),
		}},
	{typeName: "policy_route", desc: "Transmission > Routing > Policy routing. Rules are ordered by position; a rule may not overlap an existing one (router error 36002).",
		endpoint: "/admin/policy_route?form=policy_route", key: "name", position: "index", fields: []field{
			str("name"), str("service_type").required().about("service name, e.g. ALL"), str("src_ipgroup").required().about("ip_group name"),
			str("dst_ipgroup").required().about("ip_group name"), str("interfaces").required().about("outgoing WAN, e.g. WAN1"),
			str("timeobj").required().about("time object, Any for always"), str("mode").required().about("Priority or Only"),
			str("comment"), flag("state").dflt(true), num("position").rd("index").wr("index").about("1-based order; defaults to last"),
		}},
	{typeName: "lan", desc: "Network > LAN > Network list (one row per LAN; DHCP options of a LAN are in er605_dhcp_server for the default one). Editing the IP of the LAN you are connected through drops your connection. Creating extra LANs is untested; the default LAN is never deleted, only forgotten.",
		endpoint: "/admin/dhcps?form=lan", key: "name", rowKey: "vlanid", omit: []string{"maxleases"}, fields: []field{
			str("name"), str("ipaddr").required(), str("netmask").required(), str("vlanid").required(), str("mode").about("normal or bridge"),
		}},
	{typeName: "vlan", desc: "Network > VLAN > VLAN list (key = vlan id). Port membership is set per port with er605_vlan_port.",
		endpoint: "/admin/vlansetting?form=vlansettinglist", key: "vlan", rowKey: "vlan", fields: []field{
			str("vlan").about("VLAN id 1-4094"), str("name"), str("desc"),
			{name: "ports", k: kStr, ro: true}, {name: "users", k: kStr, ro: true},
		}},
	{typeName: "acl_rule", desc: "Firewall > Access control rule. position is not exposed (the router does not return it): new rules go last. src/dest are ip_group names (or Any/Me); zone e.g. [\"LAN\"] = LAN->WAN.",
		endpoint: "/admin/access_ctl?form=acl_inner", key: "name", position: "position", consts: map[string]any{"flag": "1", "user": "1"}, fields: []field{
			str("name"), str("policy").required().about("DROP or ACCEPT"), str("service").required().about("service name, ALL for any"),
			strs("zone").required(), str("src").required(), str("dest").required(), str("time").required().about("time object, Any for always"),
			strs("states").about("subset of new, established, invalid, related"),
			{name: "src_if", k: kStr, ro: true, read: "srcIf"}, {name: "dest_if", k: kStr, ro: true, read: "destIf"},
		}},
	{typeName: "qos", desc: "Transmission > QoS > global switch and thresholds.", endpoint: "/admin/qos?form=global",
		drop: []string{".name", ".type", ".anonymous"}, fields: []field{
			flag("qos_enable"), flag("threshold_enable"), num("qos_threshold"), num("bandwidth"), strs("interface"),
		}},
	{typeName: "qos_rule", desc: "Transmission > QoS > bandwidth control rule. Rules go last. QoS must have been saved once (er605_qos) before the router accepts rules.",
		endpoint: "/admin/qos?form=rule", key: "name", position: "position", consts: map[string]any{"ip_type": "src"}, fields: []field{
			str("name"), str("if_ping").required().about("source interface, e.g. LAN"), str("if_pong").required().about("WAN, e.g. WAN1 or WAN_ALL"),
			str("source_type").required().about("network or ip_group"), str("network"), str("ip_group"),
			num("rate_max").required().about("kbps"), num("rate_max_mate").required().about("kbps"),
			str("mode").required().about("share or priv"), str("time").required(), str("comment"), flag("enable").dflt(true),
		}},
	{typeName: "qos_tag", desc: "Transmission > QoS > traffic tagging switches.", endpoint: "/admin/qos_ctl?form=tag", fields: []field{
		flag("status1").words("enable", "disable"), flag("status2").words("enable", "disable"),
		flag("status3").words("enable", "disable"), flag("status4").words("enable", "disable"),
	}},
}
