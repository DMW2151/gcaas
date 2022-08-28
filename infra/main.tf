terraform {
  backend "pg" {
    // use a local postgreSQL backend for expediency
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

// APPLY
// terraform apply -var="digitalocean_token=${DIGITALOCEAN_TOKEN}"  -var "spaces_access_id=${DO_SPACES_KEY}" -var "spaces_secret_key=${DO_SPACES_SECRET}"       

// START ON INSTANCE
// /usr/local/bin/doctl registry login -t $DIGITALOCEAN_TOKEN
// sudo ENVIRONMENT="PRODUCTION" DO_SPACES_KEY=${DO_SPACES_KEY} DO_SPACES_SECRET=${DO_SPACES_SECRET} docker-compose up -d

// configure the digitalocean provider - assumes `digitalocean_token` set externally e.g.
provider "digitalocean" {
  token             = var.digitalocean_token // auth - general resource mgmt
  spaces_access_id  = var.spaces_access_id
  spaces_secret_key = var.spaces_secret_key
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
