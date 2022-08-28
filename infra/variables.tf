// variables //

variable "digitalocean_token" {
  type      = string
  sensitive = true
}

variable "spaces_access_id" {
  type      = string
  sensitive = true
}

variable "spaces_secret_key" {
  type      = string
  sensitive = true
}

variable "region" {
  type        = string
  description = "region to deploy all resources into"
  default     = "nyc3"
}

variable "domain" {
  type        = string
  description = "DO registered domain name to serve content thru"
  default     = "dmw2151.com"
}

variable "grpc_mgmt_port" {
  type        = string
  description = "traffic port for geocoder management service"
  default     = "50052"
}

variable "grpc_geocoder_port" {
  type        = string
  description = "traffic port for geocoder grpc service"
  default     = "50051"
}

variable "http_traffic_port" {
  type        = string
  description = "traffic port for geocoder edge http service"
  default     = "2151"
}

variable "insights_traffic_port" {
  type        = string
  description = "traffic port for redis-insights service"
  default     = "8001"
}