// resources for image management via DOCR and Docker //

// new digitalocean container registry
// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/container_registry
resource "digitalocean_container_registry" "gcaas" {
  name                   = "gcaas-reg"
  subscription_tier_slug = "basic"
  region                 = var.region
}

// DOCR credentials required for docker provider, see `main.tf`
// https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/container_registry_docker_credentials
resource "digitalocean_container_registry_docker_credentials" "gcaas" {
  registry_name = digitalocean_container_registry.gcaas.name
}

// build grpc service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_grpc" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-grpc:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../srv/cmd/grpc/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../srv/*") : filesha1(f)]))
  }
}

// build http service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_edge" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-edge:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../srv/cmd/public/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../srv/*") : filesha1(f)]))
  }
}

// push grpc service to DOCR
// resource: https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource
resource "null_resource" "docr_push_gcaas_grpc" {
  provisioner "local-exec" {
    command = "doctl registry login && docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-grpc:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_grpc.id
  }
}

// push http service to DOCR
// resource: https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource
resource "null_resource" "docr_push_gcaas_edge" {
  provisioner "local-exec" {
    command = "doctl registry login && docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-edge:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_edge.id
  }
}
