# Terraform Provider Rest

> **Fork notice** — This provider is a fork of [`magodo/terraform-provider-restful`](https://github.com/magodo/terraform-provider-restful) based on commit [`4fd3282`](https://github.com/magodo/terraform-provider-restful/commit/4fd32820b81d014bf2be16bb0257cc15edfa87e8) (v0.25.2+2, 2026-03-27).
> The provider has been renamed from `restful` to `rest` (`registry.terraform.io/laurentlesle/rest`).

This is a general Terraform provider that works with any platform exposing a RESTful API, extended with first-class support for **agentic Infrastructure-as-Code (IaC)** workflows — where AI agents autonomously plan, validate, and provision infrastructure.

## Motivation — Agentic IaC

Traditional IaC relies on human-authored configurations. Agentic IaC shifts this model: an AI agent generates, validates, and applies Terraform plans end-to-end. This requires providers that can:

- **Resolve dynamic references** across resource boundaries without hard-coded dependencies — agents compose configurations from YAML/JSON specs and need late-binding `ref:` expressions.
- **Validate external resources** before provisioning — agents must confirm that referenced subscriptions, resource groups, or GitHub repos actually exist, and enrich the plan with live attributes (e.g. fetching a resource group's `location` from the ARM API).
- **Deploy Kubernetes workloads** (Helm charts, service accounts) as part of a single Terraform run — agents orchestrate full-stack deployments, not just cloud resources.
- **Merge configuration layers** — agents build resolution contexts by merging YAML-driven config with module outputs, enabling a data-driven, composable IaC pattern.

This fork adds the primitives needed to support these workflows while preserving full backward compatibility with the upstream `restful` provider.

## Features (upstream)

- Different authentication choices: HTTP auth (basic, token), API Key auth, and OAuth2 (client credential, password credential, refresh token)
- Customized CRUD methods and paths
- Support precheck conditions
- Support polling asynchronous operations
- Partial `body` tracking: only the specified properties of the resource in the `body` attribute is tracked for diffs
- `rest_operation` resource that supports arbitrary RESTful API calls (e.g. `POST`) on create/update
- Ephemeral resource `rest_resource`
- [Write-only attributes](https://developer.hashicorp.com/terraform/plugin/framework/resources/write-only-arguments) supported
- Resource Identity supported
- List Resource supported

## Extensions (this fork)

### New resources

| Resource | Description |
|---|---|
| `rest_helm_release` | Manages Helm chart releases on a Kubernetes cluster. Supports OCI registries (with ORAS fallback for non-standard config media types), `set` / `set_sensitive` value overrides, namespace creation, and wait-for-ready semantics. |
| `rest_token` | Creates a Kubernetes ServiceAccount with a short-lived bearer token via the TokenRequest API. Automatically provisions the ServiceAccount, binds `cluster-admin` (configurable), and refreshes the token on read when it is expired or near expiry. Useful for bootstrapping authenticated access to K8s API servers from Terraform. |

### New provider functions

| Function | Description |
|---|---|
| `provider::rest::resolve` | Resolves a single `ref:` expression (e.g. `ref:resource_groups.rg1.location\|westeurope`) against a context map. Supports dot-separated path walking and optional default values after `\|`. |
| `provider::rest::resolve_map` | Recursively resolves all `ref:` expressions inside a map of resource instances. Every string value starting with `ref:` is resolved against the context; non-ref values pass through unchanged. |
| `provider::rest::merge_with_outputs` | Merges a map of resource config entries with their corresponding module outputs (keyed by the same `for_each` keys). Outputs take precedence on collision. The result is a fully-enriched resolution context layer. |
| `provider::rest::nacl_seal` | Encrypts a secret using NaCl sealed-box encryption (`crypto_box_seal` from libsodium). Accepts a plaintext string and a base64-encoded 32-byte Curve25519 public key; returns the ciphertext as a base64 string. Deterministic for the same inputs (Terraform-safe). Used by the GitHub Actions Secrets API. |
| `provider::rest::validate_externals` | Validates and enriches external resource references via read-only API calls (ARM, Microsoft Graph, GitHub). Schema-driven — supports both an external schema registry (YAML) and inline `_schema` keys. Can fetch live attributes (`_exported_attributes`) and inject them into the externals map. Raises errors on HTTP 404; supports `fail_on_warning` mode. |

### New data source

| Data source | Description |
|---|---|
| `rest_validate_externals` | Data source wrapper around `validate_externals` for use in contexts where provider functions are not available. Emits native Terraform warnings for API issues. |

### Cross-tenant authentication (`named_auth` / `auth_ref`)

The provider supports multiple independent HTTP clients within a single provider block via `named_auth`. Each entry has its own auth transport and can be referenced from any resource or data source using `auth_ref`.

```hcl
provider "rest" {
  base_url = "https://management.azure.com"
  security = { oauth2 = { client_credentials = { ... } } }  # default tenant

  named_auth = {
    tenant_b = {
      oauth2 = { client_credentials = { client_id = "...", client_secret = "...", token_url = "..." } }
    }
  }
}

resource "rest_resource" "cross_tenant" {
  auth_ref = "tenant_b"
  path     = "/subscriptions/.../resourceGroups/my-rg"
  # ...
}
```

`auth_ref` is supported on `rest_resource`, `rest_operation`, and `rest_resource` data source.

### Provider-level configuration additions

The provider block accepts additional optional attributes to supply tokens for external validation:

- `arm_token` — Azure Resource Manager bearer token
- `arm_tenant_tokens` — Map of tenant ID to ARM token for cross-tenant access
- `graph_token` — Microsoft Graph bearer token
- `github_token` — GitHub API token
- `fail_on_warning` — When `true`, validation warnings are promoted to hard errors

These tokens are also read from environment variables as a fallback.

The provider can now be configured **without a `base_url`** when used purely for its functions (e.g. `resolve`, `validate_externals`), skipping HTTP client initialization.

## Requirement

`terraform-provider-rest` has following assumptions about the API:

- The API is expected to support the following HTTP methods:
    - `POST`/`PUT`: create the resource
    - `GET`: read the resource
    - `PUT`/`PATCH`/`POST`: update the resource
    - `DELETE`: remove the resource
- The API content type is `application/json`
- The resource should have a unique identifier (e.g. `/foos/foo1`).

Regarding the users, as `terraform-provider-rest` is essentially just a terraform-wrapped API client, practitioners have to know the details of the API for the target platform quite well.
