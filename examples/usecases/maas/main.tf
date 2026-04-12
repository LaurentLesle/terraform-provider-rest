# MAAS API uses 0-legged OAuth 1.0 (PLAINTEXT).
# Split your MAAS API key (consumer_key:consumer_token:token_secret) into three variables,
# or pass the combined form as maas_api_key for validate_externals.
provider "rest" {
  base_url = var.maas_url

  security = {
    oauth1 = {
      consumer_key   = var.maas_consumer_key
      consumer_token = var.maas_consumer_token
      token_secret   = var.maas_token_secret
    }
  }

  # Used by provider::rest::validate_externals to verify maas_* references.
  maas_url     = var.maas_url
  maas_api_key = var.maas_api_key
}

# ── Fabric ───────────────────────────────────────────────────────────────────
# A fabric groups VLANs together. MAAS creates a default fabric on install.
resource "rest_resource" "fabric" {
  path      = "/fabrics/"
  read_path = "$(path)$(body.id)"

  header = {
    "Content-Type" = "application/x-www-form-urlencoded"
  }

  body = {
    name        = "datacenter-east"
    description = "Primary fabric for datacenter east rack"
  }
}

# ── VLAN ─────────────────────────────────────────────────────────────────────
# VLANs belong to a fabric. The create path embeds the fabric ID directly.
# Form-encoded bodies must be flat, so fabric is passed as a scalar field.
resource "rest_resource" "vlan" {
  path      = "/fabrics/${rest_resource.fabric.output.id}/vlans/"
  read_path = "/fabrics/${rest_resource.fabric.output.id}/vlans/$(body.id)"

  header = {
    "Content-Type" = "application/x-www-form-urlencoded"
  }

  body = {
    vid  = "100"
    name = "management"
    mtu  = "1500"
  }

  depends_on = [rest_resource.fabric]
}

# ── Subnet ───────────────────────────────────────────────────────────────────
# Subnets are associated with a VLAN. MAAS manages DHCP and IP allocation.
resource "rest_resource" "subnet" {
  path      = "/subnets/"
  read_path = "$(path)$(body.id)"

  header = {
    "Content-Type" = "application/x-www-form-urlencoded"
  }

  body = {
    cidr        = "192.168.100.0/24"
    name        = "app-network"
    gateway_ip  = "192.168.100.1"
    dns_servers = "8.8.8.8"
    vlan        = tostring(rest_resource.vlan.output.id)
  }

  depends_on = [rest_resource.vlan]
}

# ── External reference validation ─────────────────────────────────────────────
# Verify that machines referenced by downstream resources actually exist in MAAS
# before provisioning anything that depends on them.
locals {
  externals = {
    maas_machines = {
      controller = {
        system_id = "abc123"
        hostname  = "rack-controller-01"
      }
    }
  }

  # Validate and enrich: adds live attributes (status_name, zone, pool) from MAAS API.
  validated_externals = provider::rest::validate_externals(
    local.externals,
    yamldecode(file("${path.module}/externals_schema.yaml"))
  )
}

# ── Agentic IaC: cross-resource resolution ───────────────────────────────────
# Build a resolution context from resource outputs so that downstream modules
# can reference MAAS resource attributes without hard-coded IDs.
locals {
  maas_context = provider::rest::merge_with_outputs(
    {
      fabrics = { "datacenter-east" = { id = null } }
      subnets = { "app-network"     = { id = null, cidr = null } }
    },
    {
      fabrics = { "datacenter-east" = rest_resource.fabric.output }
      subnets = { "app-network"     = rest_resource.subnet.output }
    }
  )
}
