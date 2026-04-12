output "fabric_id" {
  value = rest_resource.fabric.output.id
}

output "vlan_id" {
  value = rest_resource.vlan.output.id
}

output "subnet_id" {
  value = rest_resource.subnet.output.id
}

output "subnet_cidr" {
  value = rest_resource.subnet.output.cidr
}

output "maas_context" {
  value     = local.maas_context
  sensitive = false
}
