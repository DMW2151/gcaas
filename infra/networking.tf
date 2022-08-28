// networking resources //


data "http" "ip" {
  url = "https://ifconfig.me"
}

// create a small VPC
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/vpc
resource "digitalocean_vpc" "core" {
  name     = "dmw2151-services"
  region   = var.region
  ip_range = "192.168.0.0/24"
}

// all tcp (grpc and http) allowed throughout the vpc - though only expect to call HTTP thru load-balancer
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/firewall
resource "digitalocean_firewall" "allow_svc_traffic" {
  name = "geocoder-svc-allow-in-vpc"

  droplet_ids = [digitalocean_droplet.gc.id]

  inbound_rule {
    protocol         = "tcp"
    port_range            = "1-65335"
    source_addresses = [digitalocean_vpc.core.ip_range]
  }

  outbound_rule {
    protocol              = "tcp"
    port_range            = "1-65335"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/firewall
resource "digitalocean_firewall" "allow_dev" {
  name = "geocoder-svc-allow-dev"

  droplet_ids = [digitalocean_droplet.gc.id]

  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["${chomp(data.http.ip.body)}/32"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = var.grpc_mgmt_port
    source_addresses = ["${chomp(data.http.ip.body)}/32"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = var.grpc_geocoder_port
    source_addresses = ["${chomp(data.http.ip.body)}/32"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = var.http_traffic_port
    source_addresses = ["${chomp(data.http.ip.body)}/32"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = var.insights_traffic_port
    source_addresses = ["${chomp(data.http.ip.body)}/32"]
  }

}
