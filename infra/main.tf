terraform {

  // use a local postgreSQL backend - exotic!
  backend "pg" {
    conn_str = "postgres://dustinwilson:@localhost/terraform?sslmode=disable"
  }

  required_providers {

    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.0"
    }

    docker = {
      source  = "kreuzwerker/docker"
      version = "2.20.2"
    }

  }

}

// configure the digitalocean provider - assumes `digitalocean_token` set externally e.g.
// terraform apply -var="digitalocean_token=${DIGITALOCEAN_TOKEN}" -auto-approve 
provider "digitalocean" {

  // auth - general resource mgmt
  token = var.digitalocean_token

  // auth - spaces API
  spaces_access_id  = var.access_id
  spaces_secret_key = var.secret_key
}

// configure the docker provider - uses the local machine to build the image and (regrettably)
// depends on local-exec to push to DOCR
provider "docker" {
  host = "unix:///var/run/docker.sock"
}

// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/project
resource "digitalocean_project" "gcaas" {
  name        = "dmw-gcaas"
  description = "Using redis w. FT.SEARCH to provide a geocoder service"
  environment = "Production"
  resources = [
    digitalocean_loadbalancer.http_edge.urn,
    digitalocean_droplet.gc.urn,
  ]
}
