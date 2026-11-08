terraform {
  required_providers {
    er605 = {
      source = "gzamboni/er605"
    }
  }
}

# Credentials can also come from ER605_URL / ER605_USERNAME / ER605_PASSWORD.
provider "er605" {
  url      = "http://192.168.0.1" # the router's HTTPS only speaks legacy TLS
  username = "admin"
  password = var.router_password
  timeout  = 90
}

variable "router_password" {
  type      = string
  sensitive = true
}
