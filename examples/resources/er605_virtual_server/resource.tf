resource "er605_virtual_server" "web" {
  name          = "web"
  interface     = "WAN1"
  external_port = "8443"
  internal_port = "443"
  ipaddr        = "192.168.0.10"
  protocol      = "TCP"
}
