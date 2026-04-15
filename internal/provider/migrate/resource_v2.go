package migrate

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// pollAttributeV2 — V2 state stored success as a ListAttribute (same as V4).
func pollAttributeV2() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"status_locator": schema.StringAttribute{Required: true},
			"status": schema.SingleNestedAttribute{
				Required: true,
				Attributes: map[string]schema.Attribute{
					"success": schema.ListAttribute{Required: true, ElementType: types.StringType},
					"pending": schema.ListAttribute{Optional: true, ElementType: types.StringType},
				},
			},
			"url_locator":       schema.StringAttribute{Optional: true},
			"header":            schema.MapAttribute{ElementType: types.StringType, Optional: true},
			"default_delay_sec": schema.Int64Attribute{Optional: true, Computed: true},
		},
	}
}

func precheckAttributeV2(pathIsRequired bool) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional: true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"mutex": schema.StringAttribute{Optional: true},
				"api": schema.SingleNestedAttribute{
					Optional: true,
					Attributes: map[string]schema.Attribute{
						"status_locator": schema.StringAttribute{Required: true},
						"status": schema.SingleNestedAttribute{
							Required: true,
							Attributes: map[string]schema.Attribute{
								"success": schema.ListAttribute{Required: true, ElementType: types.StringType},
								"pending": schema.ListAttribute{Optional: true, ElementType: types.StringType},
							},
						},
						"path":              schema.StringAttribute{Required: pathIsRequired, Optional: !pathIsRequired},
						"query":             schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
						"header":            schema.MapAttribute{ElementType: types.StringType, Optional: true},
						"default_delay_sec": schema.Int64Attribute{Optional: true, Computed: true},
					},
				},
			},
		},
	}
}

func retryAttributeV2() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"status_locator": schema.StringAttribute{Required: true},
			"status": schema.SingleNestedAttribute{
				Required: true,
				Attributes: map[string]schema.Attribute{
					"success": schema.ListAttribute{Required: true, ElementType: types.StringType},
					"pending": schema.ListAttribute{Optional: true, ElementType: types.StringType},
				},
			},
			"count":           schema.Int64Attribute{Optional: true},
			"wait_in_sec":     schema.Int64Attribute{Optional: true},
			"max_wait_in_sec": schema.Int64Attribute{Optional: true},
		},
	}
}

var ResourceSchemaV2 = schema.Schema{
	Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true},
		"path": schema.StringAttribute{Required: true},

		"auth_ref":                    schema.StringAttribute{Optional: true},
		"body_value_case_insensitive": schema.BoolAttribute{Optional: true},

		"create_selector":      schema.StringAttribute{Optional: true},
		"read_selector":        schema.StringAttribute{Optional: true},
		"read_response_template": schema.StringAttribute{Optional: true},

		"read_path":   schema.StringAttribute{Optional: true},
		"update_path": schema.StringAttribute{Optional: true},
		"delete_path": schema.StringAttribute{Optional: true},

		"create_method": schema.StringAttribute{Optional: true},
		"update_method": schema.StringAttribute{Optional: true},
		"delete_method": schema.StringAttribute{Optional: true},

		"body":          schema.DynamicAttribute{Required: true},
		"ephemeral_body": schema.DynamicAttribute{Optional: true, WriteOnly: true},
		"delete_body":   schema.DynamicAttribute{Optional: true},
		"delete_body_raw": schema.StringAttribute{Optional: true},

		"update_body_patches": schema.ListNestedAttribute{
			Optional: true,
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"path":     schema.StringAttribute{Required: true},
					"raw_json": schema.StringAttribute{Optional: true},
					"removed":  schema.BoolAttribute{Optional: true},
				},
			},
		},

		"poll_create": pollAttributeV2(),
		"poll_update": pollAttributeV2(),
		"poll_delete": pollAttributeV2(),

		"precheck_create": precheckAttributeV2(true),
		"precheck_update": precheckAttributeV2(false),
		"precheck_delete": precheckAttributeV2(false),

		"post_create_read": schema.SingleNestedAttribute{
			Optional: true,
			Attributes: map[string]schema.Attribute{
				"path":     schema.StringAttribute{Required: true},
				"query":    schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
				"header":   schema.MapAttribute{ElementType: types.StringType, Optional: true},
				"selector": schema.StringAttribute{Optional: true},
			},
		},

		"retry": retryAttributeV2(),

		"write_only_attrs":     schema.ListAttribute{Optional: true, ElementType: types.StringType},
		"merge_patch_disabled": schema.BoolAttribute{Optional: true},
		"ignore_body_changes":  schema.SetAttribute{Optional: true, ElementType: types.StringType},

		"query":        schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"create_query": schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"read_query":   schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"update_query": schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"delete_query": schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},

		"header":           schema.MapAttribute{ElementType: types.StringType, Optional: true},
		"ephemeral_header": schema.DynamicAttribute{Optional: true, WriteOnly: true},
		"create_header":    schema.MapAttribute{ElementType: types.StringType, Optional: true},
		"read_header":      schema.MapAttribute{ElementType: types.StringType, Optional: true},
		"update_header":    schema.MapAttribute{ElementType: types.StringType, Optional: true},
		"delete_header":    schema.MapAttribute{ElementType: types.StringType, Optional: true},

		"check_existance":  schema.BoolAttribute{Optional: true},
		"force_new_attrs":  schema.SetAttribute{Optional: true, ElementType: types.StringType},
		"output_attrs":     schema.SetAttribute{Optional: true, ElementType: types.StringType},

		"use_sensitive_output": schema.BoolAttribute{Optional: true},
		"output":               schema.DynamicAttribute{Computed: true},
		"sensitive_output":     schema.DynamicAttribute{Computed: true, Sensitive: true},
	},
}

// ResourceDataV2 is the struct for deserializing V2 state (success as string)
type ResourceDataV2 struct {
	ID   types.String `tfsdk:"id"`
	Path types.String `tfsdk:"path"`

	AuthRef                  types.String `tfsdk:"auth_ref"`
	BodyValueCaseInsensitive types.Bool   `tfsdk:"body_value_case_insensitive"`

	CreateSelector       types.String `tfsdk:"create_selector"`
	ReadSelector         types.String `tfsdk:"read_selector"`
	ReadResponseTemplate types.String `tfsdk:"read_response_template"`

	ReadPath   types.String `tfsdk:"read_path"`
	UpdatePath types.String `tfsdk:"update_path"`
	DeletePath types.String `tfsdk:"delete_path"`

	CreateMethod types.String `tfsdk:"create_method"`
	UpdateMethod types.String `tfsdk:"update_method"`
	DeleteMethod types.String `tfsdk:"delete_method"`

	Body              types.Dynamic `tfsdk:"body"`
	EphemeralBody     types.Dynamic `tfsdk:"ephemeral_body"`
	DeleteBody        types.Dynamic `tfsdk:"delete_body"`
	DeleteBodyRaw     types.String  `tfsdk:"delete_body_raw"`
	UpdateBodyPatches types.List    `tfsdk:"update_body_patches"`

	PollCreate types.Object `tfsdk:"poll_create"`
	PollUpdate types.Object `tfsdk:"poll_update"`
	PollDelete types.Object `tfsdk:"poll_delete"`

	PrecheckCreate types.List `tfsdk:"precheck_create"`
	PrecheckUpdate types.List `tfsdk:"precheck_update"`
	PrecheckDelete types.List `tfsdk:"precheck_delete"`

	PostCreateRead types.Object `tfsdk:"post_create_read"`

	Retry types.Object `tfsdk:"retry"`

	WriteOnlyAttributes types.List `tfsdk:"write_only_attrs"`
	MergePatchDisabled  types.Bool `tfsdk:"merge_patch_disabled"`
	IgnoreBodyChanges   types.Set  `tfsdk:"ignore_body_changes"`

	Query       types.Map `tfsdk:"query"`
	CreateQuery types.Map `tfsdk:"create_query"`
	ReadQuery   types.Map `tfsdk:"read_query"`
	UpdateQuery types.Map `tfsdk:"update_query"`
	DeleteQuery types.Map `tfsdk:"delete_query"`

	Header          types.Map `tfsdk:"header"`
	EphemeralHeader types.Dynamic `tfsdk:"ephemeral_header"`
	CreateHeader    types.Map `tfsdk:"create_header"`
	ReadHeader      types.Map `tfsdk:"read_header"`
	UpdateHeader    types.Map `tfsdk:"update_header"`
	DeleteHeader    types.Map `tfsdk:"delete_header"`

	CheckExistance types.Bool `tfsdk:"check_existance"`
	ForceNewAttrs  types.Set  `tfsdk:"force_new_attrs"`
	OutputAttrs    types.Set  `tfsdk:"output_attrs"`

	UseSensitiveOutput types.Bool    `tfsdk:"use_sensitive_output"`
	Output             types.Dynamic `tfsdk:"output"`
	SensitiveOutput    types.Dynamic `tfsdk:"sensitive_output"`
}
