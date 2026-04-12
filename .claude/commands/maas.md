---
description: Generate Terraform restful_resource configs from MAAS API documentation for a given MAAS version
allowed-tools: mcp__plugin_context7_context7__resolve-library-id, mcp__plugin_context7_context7__query-docs
---

# Generate MAAS Terraform Resources

Generate `restful_resource` HCL configuration blocks for MAAS (Metal as a Service) API resources.

## Arguments

`$ARGUMENTS` — optional MAAS API version (e.g. `3.4`, `2.9`). Defaults to `3.5`.

## Steps

1. Resolve the Context7 library ID for MAAS:
   - Call `resolve-library-id` with `libraryName: "MAAS"` and `query: "MAAS REST API 2.0 endpoints machines subnets fabrics vlans"`
   - Select `/canonical/maas` (highest benchmark score, High reputation)

2. Fetch endpoint documentation for the requested version:
   - Call `query-docs` with the resolved ID
   - Query: `"MAAS API ${ARGUMENTS:-3.5} REST endpoints machines subnets fabrics vlans zones domains CRUD operations authentication"`
   - Use enough tokens to cover all major resource types

3. For each resource type discovered, emit a complete HCL block following these rules:

   **Provider block** (emit once):
   ```hcl
   provider "rest" {
     base_url = "http://<maas-ip>:5240/MAAS/api/2.0"
     security = {
       oauth1 = {
         consumer_key   = var.maas_consumer_key
         consumer_token = var.maas_consumer_token
         token_secret   = var.maas_token_secret
       }
     }
     maas_url    = "http://<maas-ip>:5240/MAAS/api/2.0"
     maas_api_key = var.maas_api_key
   }
   ```

   **Resource block rules**:
   - `path` = the collection endpoint (e.g. `/api/2.0/subnets/`)
   - `read_path` = the singular endpoint if different (e.g. `/api/2.0/subnets/{id}`)
   - `create_method = "POST"` (default, omit if so)
   - `update_method = "PUT"` (default, omit if so)
   - `delete_method = "DELETE"` (default, omit if so)
   - For write operations using form-encoded bodies, add: `header = { "Content-Type" = "application/x-www-form-urlencoded" }`
   - For `?op=` actions, use `restful_operation` resource with `query = { op = ["<action>"] }`
   - Cross-resource references use `provider::rest::resolve("ref:<resource>.<key>.<field>", local.context)`

   **Example output for subnets**:
   ```hcl
   resource "restful_resource" "subnet" {
     path      = "/api/2.0/subnets/"
     read_path = "/api/2.0/subnets/{id}"
     header = {
       "Content-Type" = "application/x-www-form-urlencoded"
     }
     body = jsonencode({
       cidr       = "192.168.100.0/24"
       name       = "app-network"
       gateway_ip = "192.168.100.1"
     })
   }
   ```

4. Emit a `variables.tf` block for `maas_consumer_key`, `maas_consumer_token`, `maas_token_secret`, `maas_api_key` (all sensitive strings).

5. Note any MAAS-specific quirks found in the docs:
   - Resources using `system_id` (string) instead of numeric `id` as identifier
   - Endpoints requiring `?op=` for non-CRUD actions
   - Endpoints returning form-encoded vs JSON bodies
