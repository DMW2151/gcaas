// compute resources - droplets and load balancer (droplet) for gc service //

// requires pre-existing SSH key named `droplet` exists and is available
// data: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/data-sources/ssh_key
data "digitalocean_ssh_key" "droplet" {
  name = "droplet"
}

// load balancer takes traffic from 80/443 & pushes it to the traffic port of the target instance
// no need to do service disovery or anything here - single node w. all services
//
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/loadbalancer
resource "digitalocean_loadbalancer" "http_edge" {

  name   = "dmw2151-geocoder-http-edge-svc"
  region = var.region

  droplet_ids = [digitalocean_droplet.gc.id]
  vpc_uuid    = digitalocean_vpc.core.id

  // allow insecure requests on 80; upgrade to 443
  redirect_http_to_https = true

  // only rule: 443 -> traffic port
  forwarding_rule {
    entry_port       = 443
    entry_protocol   = "https"
    target_port      = var.http_traffic_port
    target_protocol  = "http"
    certificate_name = digitalocean_certificate.cert.name
  }

  // check that /health/ is available on the one (1) service instance, only 1 svc instance :(
  // pretty much a total service health indicator
  healthcheck {
    port                     = var.http_traffic_port
    protocol                 = "http"
    path                     = "/health/"
    check_interval_seconds   = 10
    response_timeout_seconds = 5
    unhealthy_threshold      = 3
    healthy_threshold        = 3
  }
}

// server for all resources - use a single dedicated server w. 8 GB RAM + 4TB Transfer ($60/mo.) 
// to avoid dealing w. provisioning & networking over docker swarm.
//
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/droplet
resource "digitalocean_droplet" "gc" {

  // basic
  name  = "geocoder-main"
  image = "ubuntu-22-04-x64" // hardcoded b.c. of userdata script
  size  = "s-2vcpu-4gb-amd"  // see note above...

  // networking
  vpc_uuid = digitalocean_vpc.core.id
  region   = var.region

  // ssh
  ssh_keys = [data.digitalocean_ssh_key.droplet.id]

  // provisioning
  user_data = templatefile(
    "${path.module}/provisioning/droplet.sh", { DIGITALOCEAN_TOKEN = var.digitalocean_token }
  )

  // monitoring - allow DO to collect metrics w. the agent; expose in the monitoring tab
  droplet_agent = true
  monitoring    = true
}
