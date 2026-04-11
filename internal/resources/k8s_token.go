package resources

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var _ resource.Resource = &K8sTokenResource{}

type K8sTokenResource struct{}

type k8sTokenModel struct {
	ID                 types.String `tfsdk:"id"`
	Kubeconfig         types.String `tfsdk:"kubeconfig"`
	ServiceAccountName types.String `tfsdk:"service_account_name"`
	Namespace          types.String `tfsdk:"namespace"`
	TokenDuration      types.Int64  `tfsdk:"token_duration_seconds"`
	ClusterAdmin       types.Bool   `tfsdk:"cluster_admin"`
	Token              types.String `tfsdk:"token"`
	Endpoint           types.String `tfsdk:"endpoint"`
	TokenExpiry        types.String `tfsdk:"token_expiry"`
}

func NewK8sTokenResource() resource.Resource {
	return &K8sTokenResource{}
}

func (r *K8sTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_token"
}

func (r *K8sTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Creates a Kubernetes ServiceAccount with a short-lived token for API authentication.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite ID: endpoint/namespace/service_account_name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"kubeconfig": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "The kubeconfig YAML content with client certificate auth.",
			},
			"service_account_name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("terraform-rest"),
				Description: "Name of the ServiceAccount. Defaults to 'terraform-rest'.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"namespace": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("kube-system"),
				Description: "Namespace for the ServiceAccount. Defaults to 'kube-system'.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"token_duration_seconds": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(86400),
				Description: "Token duration in seconds. Defaults to 86400 (24h).",
			},
			"cluster_admin": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Bind cluster-admin role. Defaults to true.",
			},
			"token": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "The generated Bearer token.",
			},
			"endpoint": schema.StringAttribute{
				Computed:    true,
				Description: "The K8s API server endpoint from kubeconfig.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"token_expiry": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp when the current token expires.",
			},
		},
	}
}

func (r *K8sTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan k8sTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	token, endpoint, err := r.createToken(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create K8s token", err.Error())
		return
	}

	plan.Token = types.StringValue(token)
	plan.Endpoint = types.StringValue(endpoint)
	plan.TokenExpiry = types.StringValue(
		time.Now().Add(time.Duration(plan.TokenDuration.ValueInt64()) * time.Second).Format(time.RFC3339))
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
		endpoint, plan.Namespace.ValueString(), plan.ServiceAccountName.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *K8sTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state k8sTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Only regenerate when the token has expired or will expire within 5 minutes.
	// This prevents unnecessary drift on downstream resources that reference the token
	// via header (which would show changes on every plan if the token changed).
	needRefresh := true
	if !state.TokenExpiry.IsNull() && !state.TokenExpiry.IsUnknown() {
		if expiry, err := time.Parse(time.RFC3339, state.TokenExpiry.ValueString()); err == nil {
			needRefresh = time.Now().After(expiry.Add(-5 * time.Minute))
		}
	}

	if needRefresh {
		token, endpoint, err := r.createToken(ctx, state)
		if err != nil {
			tflog.Warn(ctx, "Cluster unreachable, removing from state",
				map[string]interface{}{"error": err.Error(), "id": state.ID.ValueString()})
			resp.State.RemoveResource(ctx)
			return
		}
		state.Token = types.StringValue(token)
		state.Endpoint = types.StringValue(endpoint)
		state.TokenExpiry = types.StringValue(
			time.Now().Add(time.Duration(state.TokenDuration.ValueInt64()) * time.Second).Format(time.RFC3339))
		tflog.Info(ctx, "Token refreshed (was expired or near expiry)",
			map[string]interface{}{"id": state.ID.ValueString()})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *K8sTokenResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan k8sTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	token, endpoint, err := r.createToken(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Failed to refresh K8s token", err.Error())
		return
	}

	plan.Token = types.StringValue(token)
	plan.Endpoint = types.StringValue(endpoint)
	plan.TokenExpiry = types.StringValue(
		time.Now().Add(time.Duration(plan.TokenDuration.ValueInt64()) * time.Second).Format(time.RFC3339))
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s/%s",
		endpoint, plan.Namespace.ValueString(), plan.ServiceAccountName.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *K8sTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state k8sTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client, _, err := r.buildClient(state)
	if err != nil {
		tflog.Warn(ctx, "Cannot connect to cluster for cleanup, skipping",
			map[string]interface{}{"error": err.Error()})
		return
	}

	ns := state.Namespace.ValueString()
	saName := state.ServiceAccountName.ValueString()
	_ = client.RbacV1().ClusterRoleBindings().Delete(ctx, saName+"-admin", metav1.DeleteOptions{})
	_ = client.CoreV1().ServiceAccounts(ns).Delete(ctx, saName, metav1.DeleteOptions{})
}

func (r *K8sTokenResource) buildClient(m k8sTokenModel) (*kubernetes.Clientset, string, error) {
	kubeconfig := m.Kubeconfig.ValueString()

	tmpFile, err := os.CreateTemp("", "kubeconfig-*.yaml")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp kubeconfig: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString(kubeconfig); err != nil {
		_ = tmpFile.Close()
		return nil, "", fmt.Errorf("writing kubeconfig: %w", err)
	}
	_ = tmpFile.Close()

	config, err := clientcmd.BuildConfigFromFlags("", tmpFile.Name())
	if err != nil {
		return nil, "", fmt.Errorf("parsing kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", fmt.Errorf("creating K8s client: %w", err)
	}

	return clientset, config.Host, nil
}

func (r *K8sTokenResource) createToken(ctx context.Context, m k8sTokenModel) (string, string, error) {
	client, endpoint, err := r.buildClient(m)
	if err != nil {
		return "", "", err
	}

	ns := m.Namespace.ValueString()
	saName := m.ServiceAccountName.ValueString()

	// Ensure ServiceAccount exists
	_, err = client.CoreV1().ServiceAccounts(ns).Get(ctx, saName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.CoreV1().ServiceAccounts(ns).Create(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns},
		}, metav1.CreateOptions{})
	}
	if err != nil {
		return "", "", fmt.Errorf("ensuring ServiceAccount %s/%s: %w", ns, saName, err)
	}

	// Ensure ClusterRoleBinding exists
	if m.ClusterAdmin.ValueBool() {
		crbName := saName + "-admin"
		_, err = client.RbacV1().ClusterRoleBindings().Get(ctx, crbName, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			_, err = client.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: crbName},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
				Subjects: []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      saName,
					Namespace: ns,
				}},
			}, metav1.CreateOptions{})
		}
		if err != nil {
			return "", "", fmt.Errorf("ensuring ClusterRoleBinding %s: %w", crbName, err)
		}
	}

	// Create token via TokenRequest API
	durationSeconds := m.TokenDuration.ValueInt64()
	tokenReq, err := client.CoreV1().ServiceAccounts(ns).CreateToken(ctx, saName,
		&authv1.TokenRequest{
			Spec: authv1.TokenRequestSpec{
				ExpirationSeconds: &durationSeconds,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return "", "", fmt.Errorf("creating token for %s/%s: %w", ns, saName, err)
	}

	return tokenReq.Status.Token, endpoint, nil
}
