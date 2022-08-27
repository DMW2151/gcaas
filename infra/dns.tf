// resources for domain name management //

// requires pre-existing domain using DO nameservers
// data: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/data-sources/domain
data "digitalocean_domain" "target" {
  name = var.domain
}

// generate wildcard cert for the domain
// resources: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/certificate
resource "digitalocean_certificate" "cert" {
  name    = "do-${data.digitalocean_domain.target.name}-lets-encrypt-cert"
  type    = "lets_encrypt"
  domains = [data.digitalocean_domain.target.name, "*.${data.digitalocean_domain.target.name}"]
}

// record for the edge api - public use
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/record
resource "digitalocean_record" "gc" {
  domain = data.digitalocean_domain.target.id
  type   = "A"
  name   = "gc"
  value  = digitalocean_loadbalancer.http_edge.ip
  ttl    = 300
}

// record for the grpc api - private use - "safe" only because we're on a single node
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/record
resource "digitalocean_record" "gc_grpc" {
  domain = data.digitalocean_domain.target.id
  type   = "A"
  name   = "gc-grpc"
  value  = digitalocean_droplet.gc.ipv4_address
  ttl    = 300
}
