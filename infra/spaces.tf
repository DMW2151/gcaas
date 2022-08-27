// resources for spaces //

// resource: https://registry.terraform.io/providers/digitalocean/digitalocean/latest/docs/resources/spaces_bucket
resource "digitalocean_spaces_bucket" "gc" {

  // basics 
  name   = "dmw-gcaas"
  region = var.region

  // cors
  cors_rule {
    allowed_headers = ["*"]
    allowed_methods = ["GET"]
    allowed_origins = ["*"]
    max_age_seconds = 3000
  }
}
