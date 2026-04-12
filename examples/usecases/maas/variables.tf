variable "maas_url" {
  type        = string
  description = "Base URL of the MAAS API, e.g. http://maas.example.com:5240/MAAS/api/2.0"
}

variable "maas_consumer_key" {
  type      = string
  sensitive = true
}

variable "maas_consumer_token" {
  type      = string
  sensitive = true
}

variable "maas_token_secret" {
  type      = string
  sensitive = true
}

# Combined key used by validate_externals (consumer_key:consumer_token:token_secret)
variable "maas_api_key" {
  type      = string
  sensitive = true
}
