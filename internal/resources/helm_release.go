package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
)

var _ resource.Resource = &HelmReleaseResource{}

type HelmReleaseResource struct{}

type helmReleaseModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Namespace       types.String `tfsdk:"namespace"`
	Chart           types.String `tfsdk:"chart"`
	Repository      types.String `tfsdk:"repository"`
	Version         types.String `tfsdk:"version"`
	Values          types.String `tfsdk:"values"`
	Set             types.Map    `tfsdk:"set"`
	SetSensitive    types.Map    `tfsdk:"set_sensitive"`
	KubeconfigPath  types.String `tfsdk:"kubeconfig_path"`
	KubeContext     types.String `tfsdk:"kube_context"`
	CreateNamespace types.Bool   `tfsdk:"create_namespace"`
	Wait            types.Bool   `tfsdk:"wait"`
	Timeout         types.Int64  `tfsdk:"timeout"`
	InsecureSkipTLS types.Bool   `tfsdk:"insecure_skip_tls_verify"`
	Status          types.String `tfsdk:"status"`
	AppVersion      types.String `tfsdk:"app_version"`
	ChartVersion    types.String `tfsdk:"chart_version"`
}

func NewHelmReleaseResource() resource.Resource {
	return &HelmReleaseResource{}
}

func (r *HelmReleaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_helm_release"
}

func (r *HelmReleaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Helm release on a Kubernetes cluster.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Composite ID: namespace/name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Release name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"namespace": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("default"),
				Description: "Kubernetes namespace for the release.",
			},
			"chart": schema.StringAttribute{
				Required:    true,
				Description: "Chart reference: local path, repo/chartname, or OCI URL (oci://).",
			},
			"repository": schema.StringAttribute{
				Optional:    true,
				Description: "Chart repository URL.",
			},
			"version": schema.StringAttribute{
				Optional:    true,
				Description: "Chart version constraint.",
			},
			"values": schema.StringAttribute{
				Optional:    true,
				Description: "JSON-encoded values to pass to the chart.",
			},
			"set": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Map of individual values to set (--set key=value).",
			},
			"set_sensitive": schema.MapAttribute{
				Optional:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				Description: "Map of sensitive values to set (stored encrypted in state).",
			},
			"kubeconfig_path": schema.StringAttribute{
				Optional:    true,
				Description: "Path to the kubeconfig file.",
			},
			"kube_context": schema.StringAttribute{
				Optional:    true,
				Description: "Kubeconfig context to use.",
			},
			"create_namespace": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Create the namespace if it does not exist.",
			},
			"wait": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Wait until all resources are ready.",
			},
			"timeout": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(600),
				Description: "Timeout in seconds for Helm operations.",
			},
			"insecure_skip_tls_verify": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Skip TLS certificate verification for the K8s API server.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Release status (deployed, failed, etc.).",
			},
			"app_version": schema.StringAttribute{
				Computed:    true,
				Description: "App version of the deployed chart.",
			},
			"chart_version": schema.StringAttribute{
				Computed:    true,
				Description: "Version of the deployed chart.",
			},
		},
	}
}

func helmSettings(m *helmReleaseModel) *cli.EnvSettings {
	s := cli.New()
	if !m.KubeconfigPath.IsNull() && !m.KubeconfigPath.IsUnknown() {
		s.KubeConfig = m.KubeconfigPath.ValueString()
	}
	if !m.KubeContext.IsNull() && !m.KubeContext.IsUnknown() {
		s.KubeContext = m.KubeContext.ValueString()
	}
	if !m.InsecureSkipTLS.IsNull() && m.InsecureSkipTLS.ValueBool() {
		s.KubeInsecureSkipTLSVerify = true
	}
	return s
}

func helmConfig(settings *cli.EnvSettings, namespace string) (*action.Configuration, error) {
	cfg := new(action.Configuration)

	registryClient, err := registry.NewClient(
		registry.ClientOptDebug(false),
		registry.ClientOptWriter(os.Stderr),
	)
	if err != nil {
		return nil, fmt.Errorf("creating registry client: %w", err)
	}
	cfg.RegistryClient = registryClient

	logger := log.New(os.Stderr, "helm: ", log.LstdFlags)
	if err := cfg.Init(settings.RESTClientGetter(), namespace, "secrets", logger.Printf); err != nil {
		return nil, fmt.Errorf("initializing Helm configuration: %w", err)
	}
	return cfg, nil
}

func mergedValues(m *helmReleaseModel) (map[string]interface{}, error) {
	vals := make(map[string]interface{})

	if !m.Values.IsNull() && !m.Values.IsUnknown() && m.Values.ValueString() != "" {
		if err := json.Unmarshal([]byte(m.Values.ValueString()), &vals); err != nil {
			return nil, fmt.Errorf("parsing values JSON: %w", err)
		}
	}

	if !m.Set.IsNull() && !m.Set.IsUnknown() {
		for k, v := range m.Set.Elements() {
			if sv, ok := v.(types.String); ok && !sv.IsNull() {
				setNestedValue(vals, k, sv.ValueString())
			}
		}
	}

	if !m.SetSensitive.IsNull() && !m.SetSensitive.IsUnknown() {
		for k, v := range m.SetSensitive.Elements() {
			if sv, ok := v.(types.String); ok && !sv.IsNull() {
				setNestedValue(vals, k, sv.ValueString())
			}
		}
	}

	return vals, nil
}

func setNestedValue(m map[string]interface{}, key string, value string) {
	parts := splitDotPath(key)
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = coerceValue(value)
		} else {
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			} else {
				next := make(map[string]interface{})
				current[part] = next
				current = next
			}
		}
	}
}

// coerceValue converts string values to their typed equivalents,
// matching Helm's --set behavior (booleans and numbers).
func coerceValue(s string) interface{} {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if s == "null" {
		return nil
	}
	return s
}

func splitDotPath(key string) []string {
	var parts []string
	current := ""
	for i := 0; i < len(key); i++ {
		if key[i] == '\\' && i+1 < len(key) && key[i+1] == '.' {
			current += "."
			i++
		} else if key[i] == '.' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(key[i])
		}
	}
	parts = append(parts, current)
	return parts
}

func setReleaseState(model *helmReleaseModel, rel *release.Release, ch *chart.Chart) {
	if rel != nil && rel.Info != nil {
		model.Status = types.StringValue(string(rel.Info.Status))
	}
	if ch != nil && ch.Metadata != nil {
		model.ChartVersion = types.StringValue(ch.Metadata.Version)
		model.AppVersion = types.StringValue(ch.Metadata.AppVersion)
	}
}

// locateAndLoadChart tries Helm's built-in LocateChart first. If that fails on
// an OCI chart due to unsupported config media types, it falls back to pulling
// the chart tarball directly via ORAS.
func locateAndLoadChart(ctx context.Context, chartRef string, opts *action.ChartPathOptions, settings *cli.EnvSettings) (*chart.Chart, error) {
	chartPath, err := opts.LocateChart(chartRef, settings)
	if err == nil {
		return loader.Load(chartPath)
	}

	// Only fall back for OCI charts
	if !strings.HasPrefix(chartRef, "oci://") {
		return nil, fmt.Errorf("locating chart %q: %w", chartRef, err)
	}

	// Direct OCI pull via ORAS — bypasses Helm's config media type check
	ch, oraErr := pullOCIChart(ctx, chartRef, opts.Version)
	if oraErr != nil {
		return nil, fmt.Errorf("OCI fallback for %q also failed: %w (original: %v)", chartRef, oraErr, err)
	}
	return ch, nil
}

// pullOCIChart fetches a Helm chart tarball from an OCI registry using ORAS,
// bypassing Helm's registry client which rejects manifests where the config
// layer isn't application/vnd.cncf.helm.config.v1+json.
func pullOCIChart(ctx context.Context, chartRef string, version string) (*chart.Chart, error) {
	// Parse oci://host/path → host/path
	ref := strings.TrimPrefix(chartRef, "oci://")

	// Append tag if version specified
	if version != "" {
		ref = ref + ":" + version
	}

	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("creating OCI repository for %q: %w", ref, err)
	}

	// Pull all layers into an in-memory store
	store := memory.New()
	desc, err := oras.Copy(ctx, repo, ref, store, "", oras.DefaultCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("pulling OCI artifact %q: %w", ref, err)
	}

	// Fetch the manifest to find the chart tarball layer
	manifestData, err := store.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	manifestBytes, err := io.ReadAll(manifestData)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	// Find the chart layer (tar+gzip or Helm chart content type)
	var chartLayer *ocispec.Descriptor
	for _, l := range manifest.Layers {
		layer := l
		if layer.MediaType == "application/tar+gzip" ||
			layer.MediaType == "application/vnd.cncf.helm.chart.content.v1.tar+gzip" {
			chartLayer = &layer
			break
		}
	}
	if chartLayer == nil {
		return nil, fmt.Errorf("no chart tarball layer found in manifest")
	}

	// Fetch the chart tarball from the store
	rc, err := store.Fetch(ctx, *chartLayer)
	if err != nil {
		return nil, fmt.Errorf("fetching chart layer: %w", err)
	}
	defer func() { _ = rc.Close() }()

	return loader.LoadArchive(rc)
}

func (r *HelmReleaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model helmReleaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ns := model.Namespace.ValueString()
	settings := helmSettings(&model)
	cfg, err := helmConfig(settings, ns)
	if err != nil {
		resp.Diagnostics.AddError("Helm config error", err.Error())
		return
	}

	install := action.NewInstall(cfg)
	install.ReleaseName = model.Name.ValueString()
	install.Namespace = ns
	install.CreateNamespace = model.CreateNamespace.ValueBool()
	install.Timeout = time.Duration(model.Timeout.ValueInt64()) * time.Second

	if !model.Wait.IsNull() && model.Wait.ValueBool() {
		install.Wait = true
	}

	if !model.Repository.IsNull() && !model.Repository.IsUnknown() {
		install.RepoURL = model.Repository.ValueString()
	}
	if !model.Version.IsNull() && !model.Version.IsUnknown() {
		install.Version = model.Version.ValueString()
	}

	ch, err := locateAndLoadChart(ctx, model.Chart.ValueString(), &install.ChartPathOptions, settings)
	if err != nil {
		resp.Diagnostics.AddError("Chart locate error", err.Error())
		return
	}

	vals, err := mergedValues(&model)
	if err != nil {
		resp.Diagnostics.AddError("Values error", err.Error())
		return
	}

	rel, err := install.RunWithContext(ctx, ch, vals)
	if err != nil {
		resp.Diagnostics.AddError("Install error", fmt.Sprintf("Helm install failed for release %q: %s", model.Name.ValueString(), err))
		return
	}

	model.ID = types.StringValue(ns + "/" + model.Name.ValueString())
	setReleaseState(&model, rel, ch)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *HelmReleaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model helmReleaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ns := model.Namespace.ValueString()
	settings := helmSettings(&model)
	cfg, err := helmConfig(settings, ns)
	if err != nil {
		resp.Diagnostics.AddError("Helm config error", err.Error())
		return
	}

	get := action.NewGet(cfg)
	rel, err := get.Run(model.Name.ValueString())
	if err != nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if rel.Info != nil {
		model.Status = types.StringValue(string(rel.Info.Status))
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		model.ChartVersion = types.StringValue(rel.Chart.Metadata.Version)
		model.AppVersion = types.StringValue(rel.Chart.Metadata.AppVersion)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *HelmReleaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model helmReleaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ns := model.Namespace.ValueString()
	settings := helmSettings(&model)
	cfg, err := helmConfig(settings, ns)
	if err != nil {
		resp.Diagnostics.AddError("Helm config error", err.Error())
		return
	}

	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = ns
	upgrade.Timeout = time.Duration(model.Timeout.ValueInt64()) * time.Second

	if !model.Wait.IsNull() && model.Wait.ValueBool() {
		upgrade.Wait = true
	}

	if !model.Repository.IsNull() && !model.Repository.IsUnknown() {
		upgrade.RepoURL = model.Repository.ValueString()
	}
	if !model.Version.IsNull() && !model.Version.IsUnknown() {
		upgrade.Version = model.Version.ValueString()
	}

	ch, err := locateAndLoadChart(ctx, model.Chart.ValueString(), &upgrade.ChartPathOptions, settings)
	if err != nil {
		resp.Diagnostics.AddError("Chart locate error", err.Error())
		return
	}

	vals, err := mergedValues(&model)
	if err != nil {
		resp.Diagnostics.AddError("Values error", err.Error())
		return
	}

	rel, err := upgrade.RunWithContext(ctx, model.Name.ValueString(), ch, vals)
	if err != nil {
		resp.Diagnostics.AddError("Upgrade error", fmt.Sprintf("Helm upgrade failed for release %q: %s", model.Name.ValueString(), err))
		return
	}

	setReleaseState(&model, rel, ch)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *HelmReleaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model helmReleaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ns := model.Namespace.ValueString()
	settings := helmSettings(&model)
	cfg, err := helmConfig(settings, ns)
	if err != nil {
		resp.Diagnostics.AddError("Helm config error", err.Error())
		return
	}

	uninstall := action.NewUninstall(cfg)
	uninstall.Timeout = time.Duration(model.Timeout.ValueInt64()) * time.Second

	_, err = uninstall.Run(model.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Uninstall error", fmt.Sprintf("Helm uninstall failed for release %q: %s", model.Name.ValueString(), err))
		return
	}
}
