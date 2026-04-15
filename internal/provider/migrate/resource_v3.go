package migrate

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// V3 poll/precheck schema — success was a single StringAttribute.
// Changed to ListAttribute in V4 to allow multiple success sentinels.

func pollAttributeV3() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Attributes: map[string]schema.Attribute{
			"status_locator":    schema.StringAttribute{Required: true},
			"url_locator":       schema.StringAttribute{Optional: true},
			"header":            schema.MapAttribute{ElementType: types.StringType, Optional: true},
			"default_delay_sec": schema.Int64Attribute{Optional: true, Computed: true},
			"status": schema.SingleNestedAttribute{
				Required: true,
				Attributes: map[string]schema.Attribute{
					"success": schema.StringAttribute{Required: true},
					"pending": schema.ListAttribute{Optional: true, ElementType: types.StringType},
				},
			},
		},
	}
}

func precheckAttributeV3(pathIsRequired bool) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional: true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"mutex": schema.StringAttribute{Optional: true},
				"api": schema.SingleNestedAttribute{
					Optional: true,
					Attributes: map[string]schema.Attribute{
						"status_locator":    schema.StringAttribute{Required: true},
						"path":              schema.StringAttribute{Required: pathIsRequired, Optional: !pathIsRequired},
						"query":             schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
						"header":            schema.MapAttribute{ElementType: types.StringType, Optional: true},
						"default_delay_sec": schema.Int64Attribute{Optional: true, Computed: true},
						"status": schema.SingleNestedAttribute{
							Required: true,
							Attributes: map[string]schema.Attribute{
								"success": schema.StringAttribute{Required: true},
								"pending": schema.ListAttribute{Optional: true, ElementType: types.StringType},
							},
						},
					},
				},
			},
		},
	}
}

// ResourceSchemaV3 is the prior schema used for the V3→V4 state upgrader.
// It matches the schema before PollingStatus.Success was changed from a
// single string to a list of strings.
var ResourceSchemaV3 = schema.Schema{
	Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true},
		"path": schema.StringAttribute{Required: true},

		"auth_ref": schema.StringAttribute{Optional: true},

		"create_selector":       schema.StringAttribute{Optional: true},
		"read_selector":         schema.StringAttribute{Optional: true},
		"read_response_template": schema.StringAttribute{Optional: true},

		"read_path":   schema.StringAttribute{Optional: true},
		"update_path": schema.StringAttribute{Optional: true},
		"delete_path": schema.StringAttribute{Optional: true},

		"create_method": schema.StringAttribute{Optional: true},
		"update_method": schema.StringAttribute{Optional: true},
		"delete_method": schema.StringAttribute{Optional: true},

		"body":          schema.DynamicAttribute{Required: true},
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

		"poll_create": pollAttributeV3(),
		"poll_update": pollAttributeV3(),
		"poll_delete": pollAttributeV3(),

		"precheck_create": precheckAttributeV3(true),
		"precheck_update": precheckAttributeV3(false),
		"precheck_delete": precheckAttributeV3(false),

		"post_create_read": schema.SingleNestedAttribute{
			Optional: true,
			Attributes: map[string]schema.Attribute{
				"path":     schema.StringAttribute{Required: true},
				"query":    schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
				"header":   schema.MapAttribute{ElementType: types.StringType, Optional: true},
				"selector": schema.StringAttribute{Optional: true},
			},
		},

		"write_only_attrs":     schema.ListAttribute{Optional: true, ElementType: types.StringType},
		"merge_patch_disabled": schema.BoolAttribute{Optional: true},

		"query":        schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"create_query": schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"read_query":   schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"update_query": schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},
		"delete_query": schema.MapAttribute{ElementType: types.ListType{ElemType: types.StringType}, Optional: true},

		"header":           schema.MapAttribute{ElementType: types.StringType, Optional: true},
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

// ResourceDataV3 is the struct for decoding V3 state (success as single string).
type ResourceDataV3 struct {
	ID   types.String `tfsdk:"id"`
	Path types.String `tfsdk:"path"`

	AuthRef types.String `tfsdk:"auth_ref"`

	CreateSelector       types.String `tfsdk:"create_selector"`
	ReadSelector         types.String `tfsdk:"read_selector"`
	ReadResponseTemplate types.String `tfsdk:"read_response_template"`

	ReadPath   types.String `tfsdk:"read_path"`
	UpdatePath types.String `tfsdk:"update_path"`
	DeletePath types.String `tfsdk:"delete_path"`

	CreateMethod types.String `tfsdk:"create_method"`
	UpdateMethod types.String `tfsdk:"update_method"`
	DeleteMethod types.String `tfsdk:"delete_method"`

	Body          types.Dynamic `tfsdk:"body"`
	DeleteBody    types.Dynamic `tfsdk:"delete_body"`
	DeleteBodyRaw types.String  `tfsdk:"delete_body_raw"`

	UpdateBodyPatches types.List `tfsdk:"update_body_patches"`

	PollCreate types.Object `tfsdk:"poll_create"`
	PollUpdate types.Object `tfsdk:"poll_update"`
	PollDelete types.Object `tfsdk:"poll_delete"`

	PrecheckCreate types.List `tfsdk:"precheck_create"`
	PrecheckUpdate types.List `tfsdk:"precheck_update"`
	PrecheckDelete types.List `tfsdk:"precheck_delete"`

	PostCreateRead types.Object `tfsdk:"post_create_read"`

	WriteOnlyAttributes types.List `tfsdk:"write_only_attrs"`
	MergePatchDisabled  types.Bool `tfsdk:"merge_patch_disabled"`

	Query       types.Map `tfsdk:"query"`
	CreateQuery types.Map `tfsdk:"create_query"`
	ReadQuery   types.Map `tfsdk:"read_query"`
	UpdateQuery types.Map `tfsdk:"update_query"`
	DeleteQuery types.Map `tfsdk:"delete_query"`

	Header          types.Map `tfsdk:"header"`
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
