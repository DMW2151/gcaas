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

// build http service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_edge" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-edge:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../geocoder-svc/cmd/edge/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../geocoder-svc/**") : filesha1(f)]))
  }
}

// build grpc service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_geocoder" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-geocoder:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../geocoder-svc/cmd/geocoder/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../geocoder-svc/**") : filesha1(f)]))
  }
}

// build batch service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_batch" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-batch:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../geocoder-svc/cmd/batch/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../geocoder-svc/**") : filesha1(f)]))
  }
}

// build batch service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_worker" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-worker:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../geocoder-svc/cmd/worker/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../geocoder-svc/**") : filesha1(f)]))
  }
}

// build batch service image locally
// resource: https://registry.terraform.io/providers/kreuzwerker/docker/latest/docs/resources/image#build
resource "docker_image" "gcaas_mgmt" {
  name = "${digitalocean_container_registry.gcaas.endpoint}/gcaas-mgmt:0.0.1"
  build {
    path       = "${path.module}/.."
    dockerfile = "${path.module}/../geocoder-svc/cmd/mgmt/Dockerfile"
  }
  triggers = {
    src_code_hash = sha1(join("", [for f in fileset(path.module, "./../geocoder-svc/**") : filesha1(f)]))
  }
}

// push grpc service to DOCR
// resource: https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource
resource "null_resource" "docr_push_gcaas_edge" {
  provisioner "local-exec" {
    command = "docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-edge:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_edge.id
  }
}

resource "null_resource" "docr_push_gcaas_geocoder" {
  provisioner "local-exec" {
    command = "docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-geocoder:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_geocoder.id
  }
}

resource "null_resource" "docr_push_gcaas_batch" {
  provisioner "local-exec" {
    command = "docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-batch:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_batch.id
  }
}

resource "null_resource" "docr_push_gcaas_worker" {
  provisioner "local-exec" {
    command = "docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-worker:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_worker.id
  }
}

resource "null_resource" "docr_push_gcaas_mgmt" {
  provisioner "local-exec" {
    command = "docker push ${digitalocean_container_registry.gcaas.endpoint}/gcaas-mgmt:0.0.1"
  }
  triggers = {
    image_id_w_sha = docker_image.gcaas_mgmt.id
  }
}
