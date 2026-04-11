package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/LaurentLesle/terraform-provider-rest/internal/functions"
)

var _ datasource.DataSource = &ValidateExternalsDataSource{}

type ValidateExternalsDataSource struct {
	provider *Provider
}

func NewValidateExternalsDataSource(p *Provider) func() datasource.DataSource {
	return func() datasource.DataSource {
		return &ValidateExternalsDataSource{provider: p}
	}
}

func (d *ValidateExternalsDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "rest_validate_externals"
}

func (d *ValidateExternalsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Validates and enriches external resource references via read-only API calls. Emits native Terraform warnings for API issues (404, permission errors). When fail_on_warning is true on the provider, errors instead of warnings.",
		Attributes: map[string]schema.Attribute{
			"externals": schema.DynamicAttribute{
				Required:    true,
				Description: "The externals map.",
			},
			"schema_registry": schema.DynamicAttribute{
				Optional:    true,
				Description: "Schema registry map.",
			},
			"result": schema.DynamicAttribute{
				Computed:    true,
				Description: "The validated and enriched externals map.",
			},
		},
	}
}

type validateExternalsDataSourceModel struct {
	Externals      types.Dynamic `tfsdk:"externals"`
	SchemaRegistry types.Dynamic `tfsdk:"schema_registry"`
	Result         types.Dynamic `tfsdk:"result"`
}

func (d *ValidateExternalsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model validateExternalsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	underlying := model.Externals.UnderlyingValue()

	if underlying == nil || underlying.IsNull() || underlying.IsUnknown() {
		model.Result = model.Externals
		resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
		return
	}

	var registry map[string]attr.Value
	if ru := model.SchemaRegistry.UnderlyingValue(); ru != nil && !ru.IsNull() && !ru.IsUnknown() {
		registry = functions.ExtractEntries(ru)
	}

	tokens := functions.ResolveTokens(d.provider)

	enrichedData, structErrors, fatalErr := functions.ValidateAndEnrich(underlying, registry, tokens)
	if fatalErr != "" {
		resp.Diagnostics.AddError("External resource validation", fatalErr)
		return
	}
	if len(structErrors) > 0 {
		resp.Diagnostics.AddError(
			"External resource validation failed",
			strings.Join(structErrors, "\n"),
		)
		return
	}

	warnings := functions.CollectWarnings(enrichedData)
	if len(warnings) > 0 {
		if tokens.FailOnWarning {
			resp.Diagnostics.AddError(
				"External resource validation failed (fail_on_warning=true)",
				strings.Join(warnings, "\n"),
			)
			return
		}
		for _, w := range warnings {
			resp.Diagnostics.AddWarning("External resource validation", w)
		}
	}

	result, diags := functions.BuildEnrichedDynamic(enrichedData)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	model.Result = result
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (d *ValidateExternalsDataSource) Configure(_ context.Context, _ datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if d.provider == nil {
		resp.Diagnostics.AddError("Provider not configured",
			"Expected a configured provider, got nil")
	}
}
