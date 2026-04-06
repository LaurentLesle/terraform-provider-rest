package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// TestK8sTokenResource_Metadata verifies the resource type name follows naming convention.
func TestK8sTokenResource_Metadata(t *testing.T) {
	r := NewK8sTokenResource()
	req := resource.MetadataRequest{ProviderTypeName: "rest"}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), req, resp)

	if resp.TypeName != "rest_token" {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, "rest_token")
	}
}

// TestK8sTokenResource_Schema verifies the schema contains all expected attributes.
func TestK8sTokenResource_Schema(t *testing.T) {
	r := NewK8sTokenResource()
	req := resource.SchemaRequest{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), req, resp)

	expectedAttrs := []string{
		"id", "kubeconfig", "service_account_name", "namespace",
		"token_duration_seconds", "cluster_admin", "token", "endpoint", "token_expiry",
	}

	for _, attr := range expectedAttrs {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q in schema", attr)
		}
	}
}

// TestK8sTokenResource_SchemaKubeconfigSensitive verifies kubeconfig is marked sensitive.
func TestK8sTokenResource_SchemaKubeconfigSensitive(t *testing.T) {
	r := NewK8sTokenResource()
	req := resource.SchemaRequest{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), req, resp)

	attr := resp.Schema.Attributes["kubeconfig"]
	if !attr.IsSensitive() {
		t.Error("kubeconfig should be sensitive")
	}
}

// TestK8sTokenResource_SchemaTokenSensitive verifies token is marked sensitive.
func TestK8sTokenResource_SchemaTokenSensitive(t *testing.T) {
	r := NewK8sTokenResource()
	req := resource.SchemaRequest{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), req, resp)

	attr := resp.Schema.Attributes["token"]
	if !attr.IsSensitive() {
		t.Error("token should be sensitive")
	}
}

// TestHelmReleaseResource_Metadata verifies the resource type name.
func TestHelmReleaseResource_Metadata(t *testing.T) {
	r := NewHelmReleaseResource()
	req := resource.MetadataRequest{ProviderTypeName: "rest"}
	resp := &resource.MetadataResponse{}
	r.Metadata(context.Background(), req, resp)

	if resp.TypeName != "rest_helm_release" {
		t.Errorf("TypeName = %q, want %q", resp.TypeName, "rest_helm_release")
	}
}

// TestHelmReleaseResource_Schema verifies the schema contains expected attributes.
func TestHelmReleaseResource_Schema(t *testing.T) {
	r := NewHelmReleaseResource()
	req := resource.SchemaRequest{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), req, resp)

	expectedAttrs := []string{
		"id", "name", "namespace", "chart", "repository", "version",
		"values", "set", "set_sensitive", "kubeconfig_path", "kube_context",
		"create_namespace", "wait", "timeout", "insecure_skip_tls_verify",
		"status", "app_version", "chart_version",
	}

	for _, attr := range expectedAttrs {
		if _, ok := resp.Schema.Attributes[attr]; !ok {
			t.Errorf("missing attribute %q in schema", attr)
		}
	}
}

// TestHelmReleaseResource_SchemaSetSensitive verifies set_sensitive is marked sensitive.
func TestHelmReleaseResource_SchemaSetSensitive(t *testing.T) {
	r := NewHelmReleaseResource()
	req := resource.SchemaRequest{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), req, resp)

	attr := resp.Schema.Attributes["set_sensitive"]
	if !attr.IsSensitive() {
		t.Error("set_sensitive should be sensitive")
	}
}
