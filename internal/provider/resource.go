package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/LaurentLesle/terraform-provider-rest/internal/client"
	"github.com/go-resty/resty/v2"
	"github.com/LaurentLesle/terraform-provider-rest/internal/defaults"
	"github.com/LaurentLesle/terraform-provider-rest/internal/exparam"
	myvalidator "github.com/LaurentLesle/terraform-provider-rest/internal/validator"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/dynamicvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	tfpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	tffwdocs "github.com/magodo/terraform-plugin-framework-docs"
	"github.com/magodo/terraform-plugin-framework-helper/dynamic"
	"github.com/magodo/terraform-plugin-framework-helper/ephemeral"
	"github.com/magodo/terraform-plugin-framework-helper/jsonset"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type Resource struct {
	p *Provider
}

var _ resource.Resource = &Resource{}
var _ resource.ResourceWithUpgradeState = &Resource{}
var _ resource.ResourceWithIdentity = &Resource{}
var _ tffwdocs.ResourceWithRenderOption = &Resource{}

type resourceIdentityModel struct {
	ID types.String `tfsdk:"id"`
}

type resourceData struct {
	ID types.String `tfsdk:"id"`

	Path types.String `tfsdk:"path"`

	CreateSelector       types.String `tfsdk:"create_selector"`
	ReadSelector         types.String `tfsdk:"read_selector"`
	ReadResponseTemplate types.String `tfsdk:"read_response_template"`

	ReadPath   types.String `tfsdk:"read_path"`
	UpdatePath types.String `tfsdk:"update_path"`
	DeletePath types.String `tfsdk:"delete_path"`

	CreateMethod types.String `tfsdk:"create_method"`
	UpdateMethod types.String `tfsdk:"update_method"`
	DeleteMethod types.String `tfsdk:"delete_method"`

	PrecheckCreate types.List `tfsdk:"precheck_create"`
	PrecheckUpdate types.List `tfsdk:"precheck_update"`
	PrecheckDelete types.List `tfsdk:"precheck_delete"`

	Body          types.Dynamic `tfsdk:"body"`
	EphemeralBody types.Dynamic `tfsdk:"ephemeral_body"`

	DeleteBody        types.Dynamic `tfsdk:"delete_body"`
	DeleteBodyRaw     types.String  `tfsdk:"delete_body_raw"`
	UpdateBodyPatches types.List    `tfsdk:"update_body_patches"`

	PollCreate types.Object `tfsdk:"poll_create"`
	PollUpdate types.Object `tfsdk:"poll_update"`
	PollDelete types.Object `tfsdk:"poll_delete"`

	PostCreateRead types.Object `tfsdk:"post_create_read"`

	Retry types.Object `tfsdk:"retry"`

	WriteOnlyAttributes     types.List `tfsdk:"write_only_attrs"`
	IgnoreBodyChanges       types.Set  `tfsdk:"ignore_body_changes"`
	BodyValueCaseInsensitive types.Bool `tfsdk:"body_value_case_insensitive"`
	MergePatchDisabled  types.Bool `tfsdk:"merge_patch_disabled"`

	Query       types.Map `tfsdk:"query"`
	CreateQuery types.Map `tfsdk:"create_query"`
	ReadQuery   types.Map `tfsdk:"read_query"`
	UpdateQuery types.Map `tfsdk:"update_query"`
	DeleteQuery types.Map `tfsdk:"delete_query"`

	Header          types.Map `tfsdk:"header"`
	EphemeralHeader types.Map `tfsdk:"ephemeral_header"`
	CreateHeader    types.Map `tfsdk:"create_header"`
	ReadHeader      types.Map `tfsdk:"read_header"`
	UpdateHeader    types.Map `tfsdk:"update_header"`
	DeleteHeader    types.Map `tfsdk:"delete_header"`

	AuthRef types.String `tfsdk:"auth_ref"`

	CheckExistance types.Bool `tfsdk:"check_existance"`
	ForceNewAttrs  types.Set  `tfsdk:"force_new_attrs"`
	OutputAttrs    types.Set  `tfsdk:"output_attrs"`

	UseSensitiveOutput types.Bool    `tfsdk:"use_sensitive_output"`
	Output             types.Dynamic `tfsdk:"output"`
	SensitiveOutput    types.Dynamic `tfsdk:"sensitive_output"`
}

type bodyPatchData struct {
	Path    types.String `tfsdk:"path"`
	RawJSON types.String `tfsdk:"raw_json"`
	Removed types.Bool   `tfsdk:"removed"`
}

type pollData struct {
	StatusLocator types.String `tfsdk:"status_locator"`
	Status        types.Object `tfsdk:"status"`
	UrlLocator    types.String `tfsdk:"url_locator"`
	Header        types.Map    `tfsdk:"header"`
	DefaultDelay  types.Int64  `tfsdk:"default_delay_sec"`
}

type precheckData struct {
	Api   types.Object `tfsdk:"api"`
	Mutex types.String `tfsdk:"mutex"`
}

type precheckDataApi struct {
	StatusLocator types.String `tfsdk:"status_locator"`
	Status        types.Object `tfsdk:"status"`
	Path          types.String `tfsdk:"path"`
	Query         types.Map    `tfsdk:"query"`
	Header        types.Map    `tfsdk:"header"`
	DefaultDelay  types.Int64  `tfsdk:"default_delay_sec"`
}

type statusDataGo struct {
	Success []string `tfsdk:"success"`
	Pending []string `tfsdk:"pending"`
}

type resourceRetryData struct {
	ErrorMessageRegex []string    `tfsdk:"error_message_regex"`
	IntervalSeconds   types.Int64 `tfsdk:"interval_seconds"`
	MaxAttempts       types.Int64 `tfsdk:"max_attempts"`
}

type postCreateRead struct {
	Path     types.String `tfsdk:"path"`
	Query    types.Map    `tfsdk:"query"`
	Header   types.Map    `tfsdk:"header"`
	Selector types.String `tfsdk:"selector"`
}

func (r *Resource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource"
	resp.ResourceBehavior = resource.ResourceBehavior{
		// The identity contains the "body" field, which can be changed during update
		MutableIdentity: true,
	}
}

const paramFuncDescription = "Supported functions include: `escape` (URL path escape, by default applied), `unescape` (URL path unescape), `query_escape` (URL query escape), `query_unescape` (URL query unescape), `base` (filepath base), `url_path` (path segment of a URL), `trim_path` (trim `path`)."

const bodyParamDescription = " can be a string literal, or combined by the body param: `$(body.x.y.z)` that expands to the `x.y.z` property of the API body. It can add a chain of functions (applied from left to right), in the form of `$f1.f2(body)`. " + paramFuncDescription

const bodyOrPathParamDescription = "This can be a string literal, or combined by following params: path param: `$(path)` expanded to `path`, body param: `$(body.x.y.z)` expands to the `x.y.z` property of the API body. Especially for the body param, it can add a chain of functions (applied from left to right), in the form of `$f1.f2(body)`. " + paramFuncDescription

func operationOverridableAttrDescription(attr string, opkind string) string {
	return fmt.Sprintf("The %[1]s parameters that are applied to each %[2]s request. This overrides the `%[1]s` set in the resource block.", attr, opkind)
}

func resourcePrecheckAttribute(s string, pathIsRequired bool, suffixDesc string, statusLocatorSupportParam bool) schema.ListNestedAttribute {
	pathDesc := "The path used to query readiness, relative to the `base_url` of the provider."
	if suffixDesc != "" {
		pathDesc += " " + suffixDesc
	}

	var statusLocatorSuffixDesc string
	if statusLocatorSupportParam {
		statusLocatorSuffixDesc = " The `path` can contain `$(body.x.y.z)` parameter that reference property from the `state.output`."
	}

	return schema.ListNestedAttribute{
		Description:         fmt.Sprintf("An array of prechecks that need to pass prior to the %q operation.", s),
		MarkdownDescription: fmt.Sprintf("An array of prechecks that need to pass prior to the %q operation.", s),
		Optional:            true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"mutex": schema.StringAttribute{
					Description:         "The name of the mutex, which implies the resource will keep waiting until this mutex is held",
					MarkdownDescription: "The name of the mutex, which implies the resource will keep waiting until this mutex is held",
					Optional:            true,
					Validators: []validator.String{
						stringvalidator.ExactlyOneOf(
							tfpath.MatchRelative().AtParent().AtName("mutex"),
							tfpath.MatchRelative().AtParent().AtName("api"),
						),
					},
				},
				"api": schema.SingleNestedAttribute{
					Description:         "Keeps waiting until the specified API meets the success status",
					MarkdownDescription: "Keeps waiting until the specified API meets the success status",
					Optional:            true,
					Attributes: map[string]schema.Attribute{
						"status_locator": schema.StringAttribute{
							Description:         "Specifies how to discover the status property. The format is either `code` or `scope.path`, where `scope` can be either `header` or `body`, and the `path` is using the gjson syntax." + statusLocatorSuffixDesc,
							MarkdownDescription: "Specifies how to discover the status property. The format is either `code` or `scope.path`, where `scope` can be either `header` or `body`, and the `path` is using the [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)." + statusLocatorSuffixDesc,
							Required:            true,
							Validators: []validator.String{
								myvalidator.StringIsParsable("", func(s string) error {
									return validateLocator(s)
								}),
							},
						},
						"status": schema.SingleNestedAttribute{
							Description:         "The expected status sentinels for each polling state.",
							MarkdownDescription: "The expected status sentinels for each polling state.",
							Required:            true,
							Attributes: map[string]schema.Attribute{
								"success": schema.ListAttribute{
									Description:         "The expected status sentinels for success status. Any matching value terminates polling successfully.",
									MarkdownDescription: "The expected status sentinels for success status. Any matching value terminates polling successfully.",
									Required:            true,
									ElementType:         types.StringType,
								},
								"pending": schema.ListAttribute{
									Description:         "The expected status sentinels for pending status.",
									MarkdownDescription: "The expected status sentinels for pending status.",
									Optional:            true,
									ElementType:         types.StringType,
								},
							},
						},
						"path": schema.StringAttribute{
							Description:         pathDesc,
							MarkdownDescription: pathDesc,
							Required:            pathIsRequired,
							Optional:            !pathIsRequired,
						},
						"query": schema.MapAttribute{
							Description:         "The query parameters. This overrides the `query` set in the resource block.",
							MarkdownDescription: "The query parameters. This overrides the `query` set in the resource block.",
							ElementType:         types.ListType{ElemType: types.StringType},
							Optional:            true,
						},
						"header": schema.MapAttribute{
							Description:         "The header parameters. This overrides the `header` set in the resource block.",
							MarkdownDescription: "The header parameters. This overrides the `header` set in the resource block.",
							ElementType:         types.StringType,
							Optional:            true,
						},
						"default_delay_sec": schema.Int64Attribute{
							Description:         fmt.Sprintf("The interval between two pollings if there is no `Retry-After` in the response header, in second. Defaults to `%d`.", defaults.PrecheckDefaultDelayInSec),
							MarkdownDescription: fmt.Sprintf("The interval between two pollings if there is no `Retry-After` in the response header, in second. Defaults to `%d`.", defaults.PrecheckDefaultDelayInSec),
							Optional:            true,
						},
					},
					Validators: []validator.Object{
						objectvalidator.ExactlyOneOf(
							tfpath.MatchRelative().AtParent().AtName("api"),
							tfpath.MatchRelative().AtParent().AtName("mutex"),
						),
					},
				},
			},
		},
	}
}

func resourcePollAttribute(s string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Description:         fmt.Sprintf("The polling option for the %q operation", s),
		MarkdownDescription: fmt.Sprintf("The polling option for the %q operation", s),
		Optional:            true,
		Attributes: map[string]schema.Attribute{
			"status_locator": schema.StringAttribute{
				Description:         "Specifies how to discover the status property. The format is either `code` or `scope.path`, where `scope` can be either `header` or `body`, and the `path` is using the gjson syntax. The `path` can contain `$(body.x.y.z)` parameter that reference property from either the response body (for `Create`, after selector), or `state.output` (for `Read`/`Update`/`Delete`).",
				MarkdownDescription: "Specifies how to discover the status property. The format is either `code` or `scope.path`, where `scope` can be either `header` or `body`, and the `path` is using the [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md). The `path` can contain `$(body.x.y.z)` parameter that reference property from either the response body (for `Create`, after selector), or `state.output` (for `Read`/`Update`/`Delete`).",
				Required:            true,
				Validators: []validator.String{
					myvalidator.StringIsParsable("", func(s string) error {
						return validateLocator(s)
					}),
				},
			},
			"status": schema.SingleNestedAttribute{
				Description:         "The expected status sentinels for each polling state.",
				MarkdownDescription: "The expected status sentinels for each polling state.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"success": schema.ListAttribute{
						Description:         "The expected status sentinels for success status. Any matching value terminates polling successfully.",
						MarkdownDescription: "The expected status sentinels for success status. Any matching value terminates polling successfully.",
						Required:            true,
						ElementType:         types.StringType,
					},
					"pending": schema.ListAttribute{
						Description:         "The expected status sentinels for pending status.",
						MarkdownDescription: "The expected status sentinels for pending status.",
						Optional:            true,
						ElementType:         types.StringType,
					},
				},
			},
			"url_locator": schema.StringAttribute{
				Description:         "Specifies how to discover the polling url. The format can be one of `header.path` (use the property at `path` in response header), `body.path` (use the property at `path` in response body) or `exact.value` (use the exact `value`). When absent, the current operation's URL is used for polling, execpt `Create` where it fallbacks to use the path constructed by the `read_path` as the polling URL.",
				MarkdownDescription: "Specifies how to discover the polling url. The format can be one of `header.path` (use the property at `path` in response header), `body.path` (use the property at `path` in response body) or `exact.value` (use the exact `value`). When absent, the current operation's URL is used for polling, execpt `Create` where it fallbacks to use the path constructed by the `read_path` as the polling URL.",
				Optional:            true,
				Validators: []validator.String{
					myvalidator.StringIsParsable("", func(s string) error {
						return validateLocator(s)
					}),
				},
			},
			"header": schema.MapAttribute{
				Description:         "The header parameters. This overrides the `header` set in the resource block.",
				MarkdownDescription: "The header parameters. This overrides the `header` set in the resource block.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"default_delay_sec": schema.Int64Attribute{
				Description:         fmt.Sprintf("The interval between two pollings if there is no `Retry-After` in the response header, in second. Defaults to `%d`.", defaults.PollDefaultDelayInSec),
				MarkdownDescription: fmt.Sprintf("The interval between two pollings if there is no `Retry-After` in the response header, in second. Defaults to `%d`.", defaults.PollDefaultDelayInSec),
				Optional:            true,
			},
		},
	}
}

func (r *Resource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "`rest_resource` manages a rest resource.",
		MarkdownDescription: "`rest_resource` manages a rest resource.",
		Version:             4,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "The ID of the Resource.",
				MarkdownDescription: "The ID of the Resource.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"path": schema.StringAttribute{
				Description:         "The path used to create the resource, relative to the `base_url` of the provider.",
				MarkdownDescription: "The path used to create the resource, relative to the `base_url` of the provider.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},

			"create_selector": schema.StringAttribute{
				Description:         "A selector in gjson query syntax, that is used when create returns a collection of resources, to select exactly one member resource of from it. By default, the whole response body is used as the body.",
				MarkdownDescription: "A selector in [gjson query syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md#queries) query syntax, that is used when create returns a collection of resources, to select exactly one member resource of from it. By default, the whole response body is used as the body.",
				Optional:            true,
			},
			"read_selector": schema.StringAttribute{
				Description:         "A selector expression in gjson query syntax, that is used when read returns a collection of resources, to select exactly one member resource of from it. This" + bodyParamDescription + " By default, the whole response body is used as the body.",
				MarkdownDescription: "A selector expression in [gjson query syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md#queries), that is used when read returns a collection of resources, to select exactly one member resource of from it. This" + bodyParamDescription + " By default, the whole response body is used as the body.",
				Optional:            true,
			},

			"read_path": schema.StringAttribute{
				Description:         "The API path used to read the resource, which is used as the `id`. The `path` is used as the `id` instead if `read_path` is absent. " + bodyOrPathParamDescription,
				MarkdownDescription: "The API path used to read the resource, which is used as the `id`. The `path` is used as the `id` instead if `read_path` is absent. " + bodyOrPathParamDescription,
				Optional:            true,
				Validators: []validator.String{
					myvalidator.StringIsPathBuilder(),
				},
			},
			"update_path": schema.StringAttribute{
				Description:         "The API path used to update the resource. The `id` is used instead if `update_path` is absent. " + bodyOrPathParamDescription,
				MarkdownDescription: "The API path used to update the resource. The `id` is used instead if `update_path` is absent. " + bodyOrPathParamDescription,
				Optional:            true,
				Validators: []validator.String{
					myvalidator.StringIsPathBuilder(),
				},
			},
			"delete_path": schema.StringAttribute{
				Description:         "The API path used to delete the resource. The `id` is used instead if `delete_path` is absent. " + bodyOrPathParamDescription,
				MarkdownDescription: "The API path used to delete the resource. The `id` is used instead if `delete_path` is absent. " + bodyOrPathParamDescription,
				Optional:            true,
				Validators: []validator.String{
					myvalidator.StringIsPathBuilder(),
				},
			},

			"body": schema.DynamicAttribute{
				Description:         "The properties of the resource.",
				MarkdownDescription: "The properties of the resource.",
				Required:            true,
			},

			"ephemeral_body": schema.DynamicAttribute{
				Description:         "The ephemeral properties of the resource. This will be merge-patched to the `body` to construct the actual request body.",
				MarkdownDescription: "The ephemeral properties of the resource. This will be merge-patched to the `body` to construct the actual request body.",
				Optional:            true,
				WriteOnly:           true,
			},

			"delete_body": schema.DynamicAttribute{
				Description:         "The payload for the `Delete` call.",
				MarkdownDescription: "The payload for the `Delete` call.",
				Optional:            true,
				Validators: []validator.Dynamic{
					dynamicvalidator.ConflictsWith(
						tfpath.MatchRoot("delete_body_raw"),
					),
				},
			},
			"delete_body_raw": schema.StringAttribute{
				Description:         "The raw payload for the `Delete` call. It can contain `$(body.x.y.z)` parameter that reference property from the `state.output`.",
				MarkdownDescription: "The raw payload for the `Delete` call. It can contain `$(body.x.y.z)` parameter that reference property from the `state.output`.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.ConflictsWith(
						tfpath.MatchRoot("delete_body"),
					),
				},
			},

			"update_body_patches": schema.ListNestedAttribute{
				Description:         "The body patches for update only. Any change here won't cause a update API call by its own, only changes from `body` does. Note that this is almost only useful for APIs that require *after-create* attribute for an update (e.g. the resource ID).",
				MarkdownDescription: "The body patches for update only. Any change here won't cause a update API call by its own, only changes from `body` does. Note that this is almost only useful for APIs that require *after-create* attribute for an update (e.g. the resource ID).",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"path": schema.StringAttribute{
							MarkdownDescription: "The path (in [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)) to the attribute to [patch](https://github.com/tidwall/sjson?tab=readme-ov-file#set-a-value). Empty string means to patch (i.e. replace or remove) the whole update body.",
							Required:            true,
						},
						"raw_json": schema.StringAttribute{
							MarkdownDescription: "The raw json used as the patch value. It can contain `$(body.x.y.z)` parameter that reference property from the `state.output`.",
							Optional:            true,
							Validators: []validator.String{
								stringvalidator.ExactlyOneOf(
									tfpath.MatchRelative().AtParent().AtName("raw_json"),
									tfpath.MatchRelative().AtParent().AtName("removed"),
								),
							},
						},
						"removed": schema.BoolAttribute{
							MarkdownDescription: "Remove the value specified by `path` from the update body.",
							Optional:            true,
							Validators: []validator.Bool{
								boolvalidator.Equals(true),
								boolvalidator.ExactlyOneOf(
									tfpath.MatchRelative().AtParent().AtName("raw_json"),
									tfpath.MatchRelative().AtParent().AtName("removed"),
								),
							},
						},
					},
				},
			},

			"read_response_template": schema.StringAttribute{
				Description:         "The raw template for transforming the response of reading (after selector). It can contain `$(body.x.y.z)` parameter that reference property from the response. This is only used to transform the read response to the same struct as the `body`.",
				MarkdownDescription: "The raw template for transforming the response of reading (after selector). It can contain `$(body.x.y.z)` parameter that reference property from the response. This is only used to transform the read response to the same struct as the `body`.",
				Optional:            true,
			},

			"poll_create": resourcePollAttribute("Create"),
			"poll_update": resourcePollAttribute("Update"),
			"poll_delete": resourcePollAttribute("Delete"),

			"precheck_create": resourcePrecheckAttribute("Create", true, "", false),
			"precheck_update": resourcePrecheckAttribute("Update", false, "By default, the `id` of this resource is used.", true),
			"precheck_delete": resourcePrecheckAttribute("Delete", false, "By default, the `id` of this resource is used.", true),

			"post_create_read": schema.SingleNestedAttribute{
				Description:         "An additional read after creation (after polling, if any) for overriding the `$(body)` used for `read_path`, which was representing the response body of the initial create call. This is only meant to be used for APIs that only forms a resource id after the resource is completely created. One example is the AzureDevOps `project` API: A `project` is identified by a UUID, the user needs to create the project, polling the long running operation, then query the `project` by its (mutable) name, where it returns you the (immutable) UUID.",
				MarkdownDescription: "An additional read after creation (after polling, if any) for overriding the `$(body)` used for `read_path`, which was representing the response body of the initial create call. This is only meant to be used for APIs that only forms a resource id after the resource is completely created. One example is the AzureDevOps `project` API: A `project` is identified by a UUID, the user needs to create the project, polling the long running operation, then query the `project` by its (mutable) name, where it returns you the (immutable) UUID.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"path": schema.StringAttribute{
						Description:         "The API path used to read the resource. " + bodyOrPathParamDescription,
						MarkdownDescription: "The API path used to read the resource. " + bodyOrPathParamDescription,
						Required:            true,
						Validators: []validator.String{
							myvalidator.StringIsPathBuilder(),
						},
					},
					"query": schema.MapAttribute{
						Description:         operationOverridableAttrDescription("query", "post create read") + " The query value" + bodyParamDescription,
						MarkdownDescription: operationOverridableAttrDescription("query", "post create read") + " The query value" + bodyParamDescription,
						ElementType:         types.ListType{ElemType: types.StringType},
						Optional:            true,
					},
					"header": schema.MapAttribute{
						Description:         operationOverridableAttrDescription("header", "post create read") + " The header value" + bodyParamDescription,
						MarkdownDescription: operationOverridableAttrDescription("header", "post create read") + " The header value" + bodyParamDescription,
						ElementType:         types.StringType,
						Optional:            true,
					},
					"selector": schema.StringAttribute{
						Description:         "A selector expression in gjson query syntax, that is used when read returns a collection of resources, to select exactly one member resource of from it. This" + bodyParamDescription + " By default, the whole response body is used as; the body.",
						MarkdownDescription: "A selector expression in [gjson query syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md#queries), that is used when read returns a collection of resources, to select exactly one member resource of from it. This" + bodyParamDescription + " By default, the whole response body is used as the body.",
						Optional:            true,
					},
				},
			},

			"create_method": schema.StringAttribute{
				Description:         "The method used to create the resource. This overrides the `create_method` set in the provider block (defaults to POST).",
				MarkdownDescription: "The method used to create the resource. This overrides the `create_method` set in the provider block (defaults to POST).",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("PUT", "POST", "PATCH"),
				},
			},
			"update_method": schema.StringAttribute{
				Description:         "The method used to update the resource. This overrides the `update_method` set in the provider block (defaults to PUT).",
				MarkdownDescription: "The method used to update the resource. This overrides the `update_method` set in the provider block (defaults to PUT).",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("PUT", "PATCH", "POST"),
				},
			},
			"delete_method": schema.StringAttribute{
				Description:         "The method used to delete the resource. This overrides the `delete_method` set in the provider block (defaults to DELETE).",
				MarkdownDescription: "The method used to delete the resource. This overrides the `delete_method` set in the provider block (defaults to DELETE).",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("DELETE", "POST", "PUT", "PATCH"),
				},
			},
			"write_only_attrs": schema.ListAttribute{
				Description:         "A list of paths (in gjson syntax) to the attributes that are only settable, but won't be read in GET response. Prefer to use `ephemeral_body`.",
				MarkdownDescription: "A list of paths (in [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)) to the attributes that are only settable, but won't be read in GET response. Prefer to use `ephemeral_body`.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"ignore_body_changes": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "A set of body attribute paths (in gjson syntax) whose values are ignored when detecting drift. The API-returned value at each path is replaced with the last known state value, preventing Terraform from seeing changes to unmanaged properties.",
				MarkdownDescription: "A set of body attribute paths (in [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)) whose values are ignored when detecting drift. The API-returned value at each path is replaced with the last known state value, preventing Terraform from seeing changes to unmanaged properties.",
			},
			"body_value_case_insensitive": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, string values in the body returned by the API are compared case-insensitively against the last known state. If a value differs only in casing, the state value's casing is used. This prevents spurious diffs when APIs normalise casing (e.g. returning \"Standard\" when \"standard\" was configured).",
				MarkdownDescription: "When true, string values in the body returned by the API are compared case-insensitively against the last known state. If a value differs only in casing, the state value's casing is used. This prevents spurious diffs when APIs normalise casing (e.g. returning `Standard` when `standard` was configured).",
			},
			"merge_patch_disabled": schema.BoolAttribute{
				Description:         "Whether to use a JSON Merge Patch as the request body in the PATCH update? This is only effective when `update_method` is set to `PATCH`. This overrides the `merge_patch_disabled` set in the provider block (defaults to `false`).",
				MarkdownDescription: "Whether to use a JSON Merge Patch as the request body in the PATCH update? This is only effective when `update_method` is set to `PATCH`. This overrides the `merge_patch_disabled` set in the provider block (defaults to `false`).",
				Optional:            true,
			},
			"query": schema.MapAttribute{
				Description:         "The query parameters that are applied to each request. This overrides the `query` set in the provider block.",
				MarkdownDescription: "The query parameters that are applied to each request. This overrides the `query` set in the provider block.",
				ElementType:         types.ListType{ElemType: types.StringType},
				Optional:            true,
			},
			"create_query": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("query", "create"),
				MarkdownDescription: operationOverridableAttrDescription("query", "create"),
				ElementType:         types.ListType{ElemType: types.StringType},
				Optional:            true,
			},
			"update_query": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("query", "update") + " The query value" + bodyParamDescription,
				MarkdownDescription: operationOverridableAttrDescription("query", "update") + " The query value" + bodyParamDescription,
				ElementType:         types.ListType{ElemType: types.StringType},
				Optional:            true,
			},
			"read_query": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("query", "read") + " The query value" + bodyParamDescription,
				MarkdownDescription: operationOverridableAttrDescription("query", "read") + " The query value" + bodyParamDescription,
				ElementType:         types.ListType{ElemType: types.StringType},
				Optional:            true,
			},
			"delete_query": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("query", "delete") + " The query value" + bodyParamDescription,
				MarkdownDescription: operationOverridableAttrDescription("query", "delete") + " The query value" + bodyParamDescription,
				ElementType:         types.ListType{ElemType: types.StringType},
				Optional:            true,
			},
			"header": schema.MapAttribute{
				Description:         "The header parameters that are applied to each request. This overrides the `header` set in the provider block.",
				MarkdownDescription: "The header parameters that are applied to each request. This overrides the `header` set in the provider block.",
				ElementType:         types.StringType,
				Optional:            true,
			},
			"ephemeral_header": schema.MapAttribute{
				Description:         "The ephemeral header parameters. This will be merged into the `header` for API calls but NOT persisted to state.",
				MarkdownDescription: "The ephemeral header parameters. This will be merged into the `header` for API calls but NOT persisted to state.",
				ElementType:         types.StringType,
				Optional:            true,
				WriteOnly:           true,
			},
			"create_header": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("header", "create"),
				MarkdownDescription: operationOverridableAttrDescription("header", "create"),
				ElementType:         types.StringType,
				Optional:            true,
			},
			"update_header": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("header", "update") + " The header value" + bodyParamDescription,
				MarkdownDescription: operationOverridableAttrDescription("header", "update") + " The header value" + bodyParamDescription,
				ElementType:         types.StringType,
				Optional:            true,
			},
			"read_header": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("header", "read") + " The header value" + bodyParamDescription,
				MarkdownDescription: operationOverridableAttrDescription("header", "read") + " The header value" + bodyParamDescription,
				ElementType:         types.StringType,
				Optional:            true,
			},
			"delete_header": schema.MapAttribute{
				Description:         operationOverridableAttrDescription("header", "delete") + " The header value" + bodyParamDescription,
				MarkdownDescription: operationOverridableAttrDescription("header", "delete") + " The header value" + bodyParamDescription,
				ElementType:         types.StringType,
				Optional:            true,
			},
			"auth_ref": schema.StringAttribute{
				Description:         "Reference to a named_auth entry in the provider configuration. When set, this resource uses the named entry's independent HTTP client (with its own auth transport) instead of the provider's default client.",
				MarkdownDescription: "Reference to a `named_auth` entry in the provider configuration. When set, this resource uses the named entry's independent HTTP client (with its own auth transport) instead of the provider's default client.",
				Optional:            true,
			},
			"check_existance": schema.BoolAttribute{
				Description:         "Whether to check resource already existed? Defaults to `false`.",
				MarkdownDescription: "Whether to check resource already existed? Defaults to `false`.",
				Optional:            true,
			},
			"force_new_attrs": schema.SetAttribute{
				Description:         "A set of `body` attribute paths (in gjson syntax) whose value once changed, will trigger a replace of this resource. Note this only take effects when the `body` is a unknown before apply. Technically, we do a JSON merge patch and check whether the attribute path appear in the merge patch.",
				MarkdownDescription: "A set of `body` attribute paths (in [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)) whose value once changed, will trigger a replace of this resource. Note this only take effects when the `body` is a unknown before apply. Technically, we do a JSON merge patch and check whether the attribute path appear in the merge patch.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"output_attrs": schema.SetAttribute{
				Description:         "A set of `output` attribute paths (in gjson syntax) that will be exported in the `output`. If this is not specified, all attributes will be exported by `output`.",
				MarkdownDescription: "A set of `output` attribute paths (in [gjson syntax](https://github.com/tidwall/gjson/blob/master/SYNTAX.md)) that will be exported in the `output`. If this is not specified, all attributes will be exported by `output`.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"use_sensitive_output": schema.BoolAttribute{
				MarkdownDescription: "Whether to use `sensitive_output` instead of `output`. When true, the response will be stored in `sensitive_output` (which is marked as sensitive). Defaults to `false`.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"retry": schema.SingleNestedAttribute{
				Description:         "Optional retry configuration. When a Create, Update or Delete call returns a non-success response whose body matches any of the `error_message_regex` patterns, the call is retried after `interval_seconds` seconds, up to `max_attempts` times total.",
				MarkdownDescription: "Optional retry configuration. When a Create, Update or Delete call returns a non-success response whose body matches any of the `error_message_regex` patterns, the call is retried after `interval_seconds` seconds, up to `max_attempts` times total.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"error_message_regex": schema.ListAttribute{
						Description:         "A list of regular expression patterns matched against the response body. If any pattern matches, the operation is retried.",
						MarkdownDescription: "A list of regular expression patterns matched against the response body. If any pattern matches, the operation is retried.",
						Required:            true,
						ElementType:         types.StringType,
					},
					"interval_seconds": schema.Int64Attribute{
						Description:         "Seconds to wait between retries. Defaults to 10.",
						MarkdownDescription: "Seconds to wait between retries. Defaults to 10.",
						Optional:            true,
					},
					"max_attempts": schema.Int64Attribute{
						Description:         "Maximum number of attempts including the first call. Defaults to 6.",
						MarkdownDescription: "Maximum number of attempts including the first call. Defaults to 6.",
						Optional:            true,
					},
				},
			},
			"output": schema.DynamicAttribute{
				Description:         "The response body. If `ephemeral_body` get returned by API, it will be removed from `output`. This is only populated when `use_sensitive_output` is false.",
				MarkdownDescription: "The response body. If `ephemeral_body` get returned by API, it will be removed from `output`. This is only populated when `use_sensitive_output` is false.",
				Computed:            true,
			},
			"sensitive_output": schema.DynamicAttribute{
				Description:         "The response body. If `ephemeral_body` get returned by API, it will be removed from `sensitive_output`. This is only populated when `use_sensitive_output` is true.",
				MarkdownDescription: "The response body. If `ephemeral_body` get returned by API, it will be removed from `sensitive_output`. This is only populated when `use_sensitive_output` is true.",
				Computed:            true,
				Sensitive:           true,
			},
		},
	}
}

func (r *Resource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config resourceData
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	if !config.Body.IsUnknown() {
		b, err := dynamic.ToJSON(config.Body)
		if err != nil {
			resp.Diagnostics.AddError(
				"Invalid configuration",
				fmt.Sprintf("marshal body: %v", err),
			)
			return
		}

		if !config.WriteOnlyAttributes.IsUnknown() && !config.WriteOnlyAttributes.IsNull() {
			for _, ie := range config.WriteOnlyAttributes.Elements() {
				ie := ie.(types.String)
				if !ie.IsUnknown() && !ie.IsNull() {
					if !gjson.Get(string(b), ie.ValueString()).Exists() {
						resp.Diagnostics.AddError(
							"Invalid configuration",
							fmt.Sprintf(`Invalid path in "write_only_attrs": %s`, ie.String()),
						)
						return
					}
				}
			}
		}

		_, diags := ephemeral.ValidateEphemeralBody(b, config.EphemeralBody)
		resp.Diagnostics = append(resp.Diagnostics, diags...)
		if diags.HasError() {
			return
		}
	}
}

func (r *Resource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		// Resource is planned for destruction.
		// Persist auth_ref from config to private state so Delete can use it
		// even if auth_ref is not in the resource state (pre-existing resources).
		if !req.Config.Raw.IsNull() {
			var config resourceData
			if diags := req.Config.Get(ctx, &config); diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}
			if !config.AuthRef.IsNull() {
				authRefJSON, _ := json.Marshal(config.AuthRef.ValueString())
				resp.Diagnostics.Append(resp.Private.SetKey(ctx, "auth_ref", authRefJSON)...)
			}
		}
		return
	}
	if req.State.Raw.IsNull() {
		// If the entire state is null, the resource is planned for creation.
		return
	}

	var plan resourceData
	if diags := req.Plan.Get(ctx, &plan); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	var state resourceData
	if diags := req.State.Get(ctx, &state); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	var config resourceData
	if diags := req.Config.Get(ctx, &config); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// Persist auth_ref to private state so Delete can use it during replace operations,
	// even if auth_ref is not yet in the resource state (pre-existing resources).
	if !plan.AuthRef.IsNull() {
		authRefJSON, _ := json.Marshal(plan.AuthRef.ValueString())
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, "auth_ref", authRefJSON)...)
	}

	defer func() {
		resp.Plan.Set(ctx, plan)
	}()

	// Set require replace if force new attributes have changed
	if !plan.ForceNewAttrs.IsUnknown() && !plan.Body.IsUnknown() {
		var forceNewAttrs []types.String
		if diags := plan.ForceNewAttrs.ElementsAs(ctx, &forceNewAttrs, false); diags != nil {
			resp.Diagnostics.Append(diags...)
			return
		}
		var knownForceNewAttrs []string
		for _, attr := range forceNewAttrs {
			if attr.IsUnknown() {
				continue
			}
			knownForceNewAttrs = append(knownForceNewAttrs, attr.ValueString())
		}

		if len(knownForceNewAttrs) != 0 {
			var state resourceData
			if diags := req.State.Get(ctx, &state); diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}

			originJson, err := dynamic.ToJSON(state.Body)
			if err != nil {
				resp.Diagnostics.AddError(
					"ModifyPlan failed",
					fmt.Sprintf("marshaling state body: %v", err),
				)
			}

			modifiedJson, err := dynamic.ToJSON(plan.Body)
			if err != nil {
				resp.Diagnostics.AddError(
					"ModifyPlan failed",
					fmt.Sprintf("marshaling plan body: %v", err),
				)
			}

			patch, err := jsonpatch.CreateMergePatch(originJson, modifiedJson)
			if err != nil {
				resp.Diagnostics.AddError("failed to create merge patch", err.Error())
				return
			}
			for _, attr := range knownForceNewAttrs {
				result := gjson.Get(string(patch), attr)
				if result.Exists() {
					resp.RequiresReplace = []tfpath.Path{tfpath.Root("body")}
					break
				}
			}
		}
	}

	// Set output as unknown to trigger a plan diff, if ephemral body has changed
	diff, diags := ephemeral.Diff(ctx, req.Private, config.EphemeralBody)
	resp.Diagnostics = append(resp.Diagnostics, diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if diff {
		tflog.Info(ctx, `"ephemeral_body" has changed`)
		// Mark the appropriate output as unknown based on use_sensitive_output
		plan.Output = types.DynamicUnknown()
	}
}

// getOutput returns the appropriate output (sensitive or normal) based on use_sensitive_output
func (r Resource) getOutput(data resourceData) types.Dynamic {
	if data.UseSensitiveOutput.ValueBool() {
		return data.SensitiveOutput
	}
	return data.Output
}

func (r *Resource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	providerData, ok := req.ProviderData.(providerData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("got: %T.", req.ProviderData),
		)
		return
	}
	if diags := providerData.provider.Init(ctx, providerData.config); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	r.p = providerData.provider
}

func (r Resource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan resourceData
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	c, err := r.p.ClientForAuthRef(plan.AuthRef.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve auth_ref", err.Error())
		return
	}
	c.SetLoggerContext(ctx)

	var config resourceData
	diags = req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	tflog.Info(ctx, "Create a resource", map[string]any{"path": plan.Path.ValueString()})

	// WriteOnly attributes (e.g. ephemeral_header) are only in config, not plan.
	// Copy them so ForResourceCreate can merge them into request headers.
	plan.EphemeralHeader = config.EphemeralHeader

	opt, diags := r.p.apiOpt.ForResourceCreate(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	if plan.CheckExistance.ValueBool() {
		// For POST-based resources the create path is typically a collection
		// endpoint (e.g. /variables) that always returns 200. Use read_path
		// (the specific resource URL) for the existence check when available
		// and its expansion doesn't require the response body.
		checkPath := plan.Path.ValueString()
		if !plan.ReadPath.IsNull() {
			if expanded, err := exparam.ExpandBodyOrPath(plan.ReadPath.ValueString(), plan.Path.ValueString(), nil); err == nil {
				checkPath = expanded
			}
		}
		opt, diags := r.p.apiOpt.ForResourceRead(ctx, plan, nil)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}
		response, err := c.Read(ctx, checkPath, *opt)
		if err != nil {
			resp.Diagnostics.AddError(
				"existence check failed",
				err.Error(),
			)
			return
		}
		if response.StatusCode() != http.StatusNotFound {
			// Resource already exists — adopt it into state instead of
			// creating a duplicate. Set the resource ID and run the Read
			// path to populate state from the existing resource.
			resourceId := checkPath
			plan.ID = types.StringValue(resourceId)
			plan.Output = types.DynamicNull()
			plan.SensitiveOutput = types.DynamicNull()
			diags = resp.State.Set(ctx, plan)
			resp.Diagnostics.Append(diags...)
			if diags.HasError() {
				return
			}
			rreq := resource.ReadRequest{
				State:        resp.State,
				Private:      resp.Private,
				Identity:     req.Identity,
				ProviderMeta: req.ProviderMeta,
			}
			rresp := resource.ReadResponse{
				State:       resp.State,
				Diagnostics: resp.Diagnostics,
				Identity:    resp.Identity,
			}
			r.read(ctx, rreq, &rresp, true)
			resp.State = rresp.State
			resp.Diagnostics = rresp.Diagnostics
			resp.Identity = rresp.Identity
			return
		}
	}

	// Precheck
	if !plan.PrecheckCreate.IsNull() {
		unlockFunc, diags := precheck(ctx, c, r.p.apiOpt, "", opt.Header, opt.Query, plan.PrecheckCreate, basetypes.NewDynamicNull())
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		defer unlockFunc()
	}

	// Build the body
	b, err := dynamic.ToJSON(plan.Body)
	if err != nil {
		resp.Diagnostics.AddError(
			`marshaling "body"`,
			err.Error(),
		)
		return
	}

	var eb []byte
	if !config.EphemeralBody.IsNull() {
		eb, diags = ephemeral.ValidateEphemeralBody(b, config.EphemeralBody)
		resp.Diagnostics = append(resp.Diagnostics, diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Merge patch the ephemeral body to the body
		b, err = jsonpatch.MergePatch(b, eb)
		if err != nil {
			resp.Diagnostics.AddError(
				"Merge patching `body` with `ephemeral_body`",
				err.Error(),
			)
			return
		}
	}

	// Create the resource
	var createRetry *resourceRetryData
	if !plan.Retry.IsNull() && !plan.Retry.IsUnknown() {
		var rd resourceRetryData
		if diags := plan.Retry.As(ctx, &rd, basetypes.ObjectAsOptions{}); !diags.HasError() {
			createRetry = &rd
		}
	}
	createPath := plan.Path.ValueString()
	createBody := string(b)
	createOpt := *opt
	response, err := callWithRetry(ctx, createRetry, func() (*resty.Response, error) {
		return c.Create(ctx, createPath, createBody, createOpt)
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"calling create API",
			err.Error(),
		)
		return
	}
	if !response.IsSuccess() {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Create API returns %d", response.StatusCode()),
			apiErrorDetail(response),
		)
		return
	}

	b = response.Body()

	if sel := plan.CreateSelector.ValueString(); sel != "" {
		bodyLocator := client.BodyLocator(sel)
		sb, ok := bodyLocator.LocateValueInResp(*response)
		if !ok {
			resp.Diagnostics.AddError(
				"`create_selector` failed to select from the response",
				string(response.Body()),
			)
			return
		}
		b = []byte(sb)
	}

	// Construct the resource id, which is used as the path to:
	// - Be the fallback read path for polling (if any) if no URL locator is specified
	// - Read the resource later on
	resourceId := plan.Path.ValueString()
	if !plan.ReadPath.IsNull() {
		resourceId, err = exparam.ExpandBodyOrPath(plan.ReadPath.ValueString(), plan.Path.ValueString(), b)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to build the path for reading the resource",
				fmt.Sprintf("Can't build resource id with `read_path`: %q, `path`: %q, `body`: %q: %v", plan.ReadPath.ValueString(), plan.Path.ValueString(), string(b), err),
			)
			return
		}
	}

	output, err := dynamic.FromJSONImplied(b)
	if err != nil {
		resp.Diagnostics.AddError(
			"Evaluating `output` during Read",
			err.Error(),
		)
		return
	}

	diags = ephemeral.Set(ctx, resp.Private, eb)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	// For LRO, wait for completion
	if !plan.PollCreate.IsNull() {
		var d pollData
		if diags := plan.PollCreate.As(ctx, &d, basetypes.ObjectAsOptions{}); diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		opt, diags := r.p.apiOpt.ForPoll(ctx, opt.Header, opt.Query, d, output)
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		if opt.UrlLocator == nil {
			// Update the request URL to pointing to the resource path, which is mainly for resources whose create method is POST.
			// As it will be used to poll the resource status.
			response.Request.URL = resourceId
		}
		p, err := client.NewPollableForPoll(*response, *opt)
		if err != nil {
			resp.Diagnostics.AddError(
				"Create: Failed to build poller from the response of the initiated request",
				err.Error(),
			)
			return
		}
		if err := p.PollUntilDone(ctx, c, nil); err != nil {
			resp.Diagnostics.AddError(
				"Create: Polling failure",
				err.Error(),
			)
			return
		}
	}

	// PostCreateRead is to update the resource ID and the Output by sending a post-create only read call.
	if !plan.PostCreateRead.IsNull() {
		var pr postCreateRead
		if diags := plan.PostCreateRead.As(ctx, &pr, basetypes.ObjectAsOptions{}); diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		opt, diags := r.p.apiOpt.ForResourcePostCreateRead(ctx, plan, pr, b)
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		response, err := c.Read(ctx, pr.Path.ValueString(), *opt)
		if err != nil {
			resp.Diagnostics.AddError(
				"calling post-create read API",
				err.Error(),
			)
			return
		}
		if !response.IsSuccess() {
			resp.Diagnostics.AddError(
				fmt.Sprintf("Post-create read API returns %d", response.StatusCode()),
				apiErrorDetail(response),
			)
			return
		}

		b := response.Body()

		if sel := pr.Selector.ValueString(); sel != "" {
			sel, err = exparam.ExpandBody(sel, b)
			if err != nil {
				resp.Diagnostics.AddError(
					"Post-create read failure",
					fmt.Sprintf("Failed to expand the post-create read selector: %v", err),
				)
				return
			}
			bodyLocator := client.BodyLocator(sel)
			sb, _ := bodyLocator.LocateValueInResp(*response)
			// This means the tracked resource selected (filtered) from the response now disappears (deleted out of band).
			b = []byte(sb)
		}

		// Update the resource Id
		if !plan.ReadPath.IsNull() {
			resourceId, err = exparam.ExpandBodyOrPath(plan.ReadPath.ValueString(), plan.Path.ValueString(), b)
			if err != nil {
				resp.Diagnostics.AddError(
					"Failed to build the path for reading the resource (post-create phase)",
					fmt.Sprintf("Can't build resource id with `read_path`: %q, `path`: %q, `body`: %q: %v", plan.ReadPath.ValueString(), plan.Path.ValueString(), string(b), err),
				)
				return
			}
		}

		// Update the output
		output, err = dynamic.FromJSONImplied(b)
		if err != nil {
			resp.Diagnostics.AddError(
				"Evaluating `output` during Read",
				err.Error(),
			)
			return
		}
	}

	// Set resource ID
	plan.ID = types.StringValue(resourceId)

	// Temporarily set the output here, so that the Read at the end can expand the `$(body)` parameters.
	// Populate the appropriate output based on use_sensitive_output
	if plan.UseSensitiveOutput.ValueBool() {
		plan.SensitiveOutput = output
		plan.Output = types.DynamicNull()
	} else {
		plan.Output = output
		plan.SensitiveOutput = types.DynamicNull()
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	// Persist auth_ref to private state for future destroy operations.
	if !plan.AuthRef.IsNull() {
		authRefJSON, _ := json.Marshal(plan.AuthRef.ValueString())
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, "auth_ref", authRefJSON)...)
	}

	rreq := resource.ReadRequest{
		State:        resp.State,
		Private:      resp.Private,
		Identity:     req.Identity,
		ProviderMeta: req.ProviderMeta,
	}
	rresp := resource.ReadResponse{
		State:       resp.State,
		Diagnostics: resp.Diagnostics,
		Identity:    resp.Identity,
	}
	r.read(ctx, rreq, &rresp, false)

	resp.State = rresp.State
	resp.Diagnostics = rresp.Diagnostics
	resp.Identity = rresp.Identity
}

func (r Resource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.read(ctx, req, resp, true)
}

func (r Resource) read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse, updateBody bool) {
	var state resourceData
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	// Pre-populate resource identity from current state so that early returns
	// (e.g. 404 → RemoveResource) don't leave it null. The terraform-plugin-
	// framework checks for non-null identity after Read returns, even when the
	// resource was removed from state.
	if resp.Identity != nil {
		r.setIdentityFromState(ctx, resp, state)
	}

	c, err := r.p.ClientForAuthRef(state.AuthRef.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve auth_ref", err.Error())
		return
	}
	c.SetLoggerContext(ctx)

	if updateBody {
		tflog.Info(ctx, "Read a resource", map[string]any{"id": state.ID.ValueString()})
	}

	stateOutput, err := dynamic.ToJSON(r.getOutput(state))
	if err != nil {
		resp.Diagnostics.AddError(
			"Read failure",
			fmt.Sprintf("marshal state output: %v", err),
		)
		return
	}

	opt, diags := r.p.apiOpt.ForResourceRead(ctx, state, stateOutput)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	response, err := c.Read(ctx, state.ID.ValueString(), *opt)
	if err != nil {
		resp.Diagnostics.AddError(
			"calling read API",
			err.Error(),
		)
		return
	}
	if response.StatusCode() == http.StatusNotFound {
		resp.State.RemoveResource(ctx)
		return
	}
	if !response.IsSuccess() {
		resp.Diagnostics.AddError(
			fmt.Sprintf("Read API returns %d", response.StatusCode()),
			apiErrorDetail(response),
		)
		return
	}

	b := response.Body()

	if sel := state.ReadSelector.ValueString(); sel != "" {
		sel, err = exparam.ExpandBody(sel, stateOutput)
		if err != nil {
			resp.Diagnostics.AddError(
				"Read failure",
				fmt.Sprintf("Failed to expand the read selector: %v", err),
			)
			return
		}
		bodyLocator := client.BodyLocator(sel)
		sb, ok := bodyLocator.LocateValueInResp(*response)
		// This means the tracked resource selected (filtered) from the response now disappears (deleted out of band).
		if !ok {
			resp.State.RemoveResource(ctx)
			return
		}
		b = []byte(sb)
	}

	if tpl := state.ReadResponseTemplate.ValueString(); tpl != "" {
		sb, err := exparam.ExpandBody(tpl, b)
		if err != nil {
			resp.Diagnostics.AddError(
				"Read failure",
				fmt.Sprintf("Failed to expand the read response template: %v", err),
			)
			return
		}
		b = []byte(sb)
	}

	if updateBody {
		var writeOnlyAttributes []string
		diags = state.WriteOnlyAttributes.ElementsAs(ctx, &writeOnlyAttributes, false)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}

		// Update the read response by compensating the write only attributes from state
		if len(writeOnlyAttributes) != 0 {
			pb := string(b)

			stateBody, err := dynamic.ToJSON(state.Body)
			if err != nil {
				resp.Diagnostics.AddError(
					"Read failure",
					fmt.Sprintf("marshal state body: %v", err),
				)
				return
			}

			for _, path := range writeOnlyAttributes {
				if gjson.Get(string(stateBody), path).Exists() && !gjson.Get(string(b), path).Exists() {
					pb, err = sjson.Set(pb, path, gjson.Get(string(stateBody), path).Value())
					if err != nil {
						resp.Diagnostics.AddError(
							"Read failure",
							fmt.Sprintf("json set write only attr at path %q: %v", path, err),
						)
						return
					}
				}
			}
			b = []byte(pb)
		}

		// Lazily compute stateBodyBytes only when needed by ignore_body_changes or
		// body_value_case_insensitive so we avoid redundant marshaling.
		needStateBody := (!state.IgnoreBodyChanges.IsNull() && !state.IgnoreBodyChanges.IsUnknown()) ||
			state.BodyValueCaseInsensitive.ValueBool()

		var stateBodyBytes []byte
		if needStateBody {
			stateBodyBytes, err = dynamic.ToJSON(state.Body)
			if err != nil {
				resp.Diagnostics.AddError(
					"Read failure",
					fmt.Sprintf("marshal state body for drift helpers: %v", err),
				)
				return
			}
		}

		// Apply ignore_body_changes: replace API-returned values with state values
		// for the declared paths so Terraform sees no drift there.
		if !state.IgnoreBodyChanges.IsNull() && !state.IgnoreBodyChanges.IsUnknown() {
			var ignorePaths []string
			diags = state.IgnoreBodyChanges.ElementsAs(ctx, &ignorePaths, false)
			resp.Diagnostics.Append(diags...)
			if diags.HasError() {
				return
			}
			b, err = applyIgnoreBodyChanges(b, stateBodyBytes, ignorePaths)
			if err != nil {
				resp.Diagnostics.AddError("Read failure", fmt.Sprintf("ignore_body_changes: %v", err))
				return
			}
		}

		// Apply case normalization if requested.
		if state.BodyValueCaseInsensitive.ValueBool() {
			b = normalizeBodyCasing(b, stateBodyBytes)
		}

		// Strip ARM-only fields before parsing the body against the plan schema.
		// When ARM returns computed sub-fields inside tuple elements (e.g. an
		// ipConfigurations entry with id, etag, privateIPAddress, provisioningState
		// that were not sent in the plan body), FromJSON either fails on a size
		// mismatch or the FromJSONImplied fallback produces an ObjectValue where the
		// plan expected a MapValue — causing Terraform to raise "Provider produced
		// inconsistent result after apply: attribute X: tuple required".
		// Restricting the response to only fields present in the plan body ensures
		// the parsed value type is structurally identical to what was planned.
		// The original full-ARM response (b) is kept for output_attrs below.
		bForBody := b
		if !state.Body.IsNull() {
			if planBodyJSON, pErr := dynamic.ToJSON(state.Body); pErr == nil {
				if stripped, sErr := ModifyBody(string(planBodyJSON), string(b), nil); sErr == nil {
					bForBody = []byte(stripped)
				}
			}
		}

		var body types.Dynamic
		if state.Body.IsNull() {
			body, err = dynamic.FromJSONImplied(b)
		} else {
			body, err = dynamic.FromJSON(bForBody, state.Body.UnderlyingValue().Type(ctx))
		}
		if err != nil {
			// An error might occur here during refresh, when the type of the state doesn't match the remote,
			// e.g. a tuple field has different number of elements.
			// In this case, we fallback to the implied types, to make the refresh proceed and return a reasonable plan diff.
			if body, err = dynamic.FromJSONImplied(bForBody); err != nil {
				resp.Diagnostics.AddError(
					"Evaluating `body` during Read",
					err.Error(),
				)
				return
			}
		}
		state.Body = body
	}

	// Set output
	if !state.OutputAttrs.IsNull() {
		var outputAttrs []string
		diags = state.OutputAttrs.ElementsAs(ctx, &outputAttrs, false)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}
		fb, err := FilterAttrsInJSON(string(b), outputAttrs)
		if err != nil {
			resp.Diagnostics.AddError(
				"Filter `output` during Read",
				err.Error(),
			)
			return
		}
		b = []byte(fb)
	}

	eb, diags := ephemeral.GetNullBody(ctx, req.Private)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if eb != nil {
		b, err = jsonset.Difference(b, eb)
		if err != nil {
			resp.Diagnostics.AddError(
				"Removing `ephemeral_body` from `output`",
				err.Error(),
			)
			return
		}
	}
	output, err := dynamic.FromJSONImplied(b)
	if err != nil {
		resp.Diagnostics.AddError(
			"Evaluating `output` during Read",
			err.Error(),
		)
		return
	}
	// Populate the appropriate output based on use_sensitive_output
	if state.UseSensitiveOutput.ValueBool() {
		state.SensitiveOutput = output
		state.Output = types.DynamicNull()
	} else {
		state.Output = output
		state.SensitiveOutput = types.DynamicNull()
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	// Set resource identity
	impspec := ImportSpec{
		Id:   state.ID.ValueString(),
		Path: state.Path.ValueString(),
	}
	if !state.Query.IsNull() {
		q, dd := client.Query{}.TakeOrSelf(ctx, state.Query)
		if resp.Diagnostics.Append(dd...); resp.Diagnostics.HasError() {
			return
		}
		impspec.Query = url.Values(q)
	}
	if !state.Header.IsNull() {
		h, dd := client.Header{}.TakeOrSelf(ctx, state.Header)
		if resp.Diagnostics.Append(dd...); resp.Diagnostics.HasError() {
			return
		}
		impspec.Header = h
	}
	if !state.Body.IsNull() {
		body, err := dynamic.ToJSON(state.Body)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to construct resource identity",
				fmt.Sprintf("convert `body` to JSON: %v", err),
			)
			return
		}
		nullBody, err := jsonset.NullifyObject(body)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to construct resource identity",
				fmt.Sprintf("nullify `body`: %v", err),
			)
			return
		}
		if string(nullBody) != "null" {
			impspec.Body = new(json.RawMessage)
			*impspec.Body = json.RawMessage(nullBody)
		}
	}
	if !state.ReadSelector.IsNull() {
		impspec.ReadSelector = state.ReadSelector.ValueStringPointer()
	}
	if !state.ReadResponseTemplate.IsNull() {
		impspec.ReadResponseTemplate = state.ReadResponseTemplate.ValueStringPointer()
	}

	impspecJSON, err := json.Marshal(impspec)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to construct resource identity",
			fmt.Sprintf("failed to marshal the import spec: %v", err),
		)
		return
	}
	if diags := resp.Identity.Set(ctx, resourceIdentityModel{
		ID: types.StringValue(string(impspecJSON)),
	}); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
}

// setIdentityFromState builds and sets resource identity from the current state.
// This is called early in Read so that early-return code paths (404, selector miss)
// don't leave identity null — the terraform-plugin-framework requires non-null
// identity whenever IdentitySchema is declared, even when the resource is removed.
func (r Resource) setIdentityFromState(ctx context.Context, resp *resource.ReadResponse, state resourceData) {
	impspec := ImportSpec{
		Id:   state.ID.ValueString(),
		Path: state.Path.ValueString(),
	}
	if !state.Query.IsNull() {
		q, dd := client.Query{}.TakeOrSelf(ctx, state.Query)
		if dd.HasError() {
			return // best-effort; the full identity will be set at end of read
		}
		impspec.Query = url.Values(q)
	}
	impspecJSON, err := json.Marshal(impspec)
	if err != nil {
		return // best-effort
	}
	_ = resp.Identity.Set(ctx, resourceIdentityModel{
		ID: types.StringValue(string(impspecJSON)),
	})
}

func (r Resource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state resourceData
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	var config resourceData
	diags = req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	var plan resourceData
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	c, err := r.p.ClientForAuthRef(plan.AuthRef.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve auth_ref", err.Error())
		return
	}
	c.SetLoggerContext(ctx)

	tflog.Info(ctx, "Update a resource", map[string]any{"id": state.ID.ValueString()})

	// WriteOnly attributes (e.g. ephemeral_header) are only in config, not plan.
	plan.EphemeralHeader = config.EphemeralHeader

	stateOutput, err := dynamic.ToJSON(r.getOutput(state))
	if err != nil {
		resp.Diagnostics.AddError(
			"Read failure",
			fmt.Sprintf("marshal state output: %v", err),
		)
		return
	}

	opt, diags := r.p.apiOpt.ForResourceUpdate(ctx, plan, stateOutput)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	stateBody, err := dynamic.ToJSON(state.Body)
	if err != nil {
		resp.Diagnostics.AddError(
			"Update failure",
			fmt.Sprintf("marshaling state body: %v", err),
		)
		return
	}
	planBody, err := dynamic.ToJSON(plan.Body)
	if err != nil {
		resp.Diagnostics.AddError(
			"Update failure",
			fmt.Sprintf("marshaling plan body: %v", err),
		)
		return
	}

	// Optionally patch the body with the update_body_patches.
	var patches []bodyPatchData
	if diags := plan.UpdateBodyPatches.ElementsAs(ctx, &patches, false); diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	if len(patches) != 0 {
		planBodyStr := string(planBody)
		for i, patch := range patches {
			switch {
			case !patch.Removed.IsNull():
				if path := patch.Path.ValueString(); path == "" {
					planBodyStr = "{}"
				} else {
					planBodyStr, err = sjson.Delete(planBodyStr, patch.Path.ValueString())
					if err != nil {
						resp.Diagnostics.AddError(
							fmt.Sprintf("Failed to delete json for the %d-th patch at path %q", i, patch.Path.ValueString()),
							err.Error(),
						)
						return
					}
				}
			case !patch.RawJSON.IsNull():
				pv, err := exparam.ExpandBody(patch.RawJSON.ValueString(), stateOutput)
				if err != nil {
					resp.Diagnostics.AddError(
						fmt.Sprintf("Failed to expand the %d-th patch for expression params", i),
						err.Error(),
					)
					return
				}

				if path := patch.Path.ValueString(); path == "" {
					planBodyStr = pv
				} else {
					planBodyStr, err = sjson.SetRaw(planBodyStr, patch.Path.ValueString(), pv)
					if err != nil {
						resp.Diagnostics.AddError(
							fmt.Sprintf("Failed to set json for the %d-th patch with %q", i, pv),
							err.Error(),
						)
						return
					}
				}
			}
		}
		planBody = []byte(planBodyStr)
	}

	// Optionally patch the body with emphemeral_body
	var eb []byte
	ephemeralDiff, diags := ephemeral.Diff(ctx, req.Private, config.EphemeralBody)
	resp.Diagnostics = append(resp.Diagnostics, diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if ephemeralDiff {
		eb, diags = ephemeral.ValidateEphemeralBody(planBody, config.EphemeralBody)
		resp.Diagnostics = append(resp.Diagnostics, diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		// Merge patch the ephemeral body to the body
		if len(eb) != 0 {
			planBody, err = jsonpatch.MergePatch(planBody, eb)
			if err != nil {
				resp.Diagnostics.AddError(
					"Merge patching `body` with `ephemeral_body`",
					err.Error(),
				)
				return
			}
		}
	}

	// Invoke API to Update the resource only when there are changes in the body (regardless of the TF type diff).
	if string(stateBody) != string(planBody) || ephemeralDiff {
		// Precheck
		if !plan.PrecheckUpdate.IsNull() {
			unlockFunc, diags := precheck(ctx, c, r.p.apiOpt, state.ID.ValueString(), opt.Header, opt.Query, plan.PrecheckUpdate, r.getOutput(state))
			if diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}
			defer unlockFunc()
		}

		if opt.Method == "PATCH" && !opt.MergePatchDisabled {
			stateBodyJSON, err := dynamic.ToJSON(state.Body)
			if err != nil {
				resp.Diagnostics.AddError(
					"Update failure",
					fmt.Sprintf("marshaling state body: %v", err),
				)
				return
			}
			b, err := jsonpatch.CreateMergePatch(stateBodyJSON, planBody)
			if err != nil {
				resp.Diagnostics.AddError(
					"Update failure",
					fmt.Sprintf("failed to create a merge patch: %s", err.Error()),
				)
				return
			}
			planBody = b
		}

		path := plan.ID.ValueString()
		if !plan.UpdatePath.IsNull() {
			output, err := dynamic.ToJSON(r.getOutput(state))
			if err != nil {
				resp.Diagnostics.AddError(
					"Failed to marshal json for `output`",
					err.Error(),
				)
				return
			}
			path, err = exparam.ExpandBodyOrPath(plan.UpdatePath.ValueString(), plan.Path.ValueString(), output)
			if err != nil {
				resp.Diagnostics.AddError(
					"Failed to build the path for updating the resource",
					fmt.Sprintf("Can't build path with `update_path`: %q, `path`: %q, `body`: %q", plan.UpdatePath.ValueString(), plan.Path.ValueString(), output),
				)
				return
			}
		}

		var updateRetry *resourceRetryData
		if !plan.Retry.IsNull() && !plan.Retry.IsUnknown() {
			var rd resourceRetryData
			if diags := plan.Retry.As(ctx, &rd, basetypes.ObjectAsOptions{}); !diags.HasError() {
				updateRetry = &rd
			}
		}
		updatePath := path
		updateBody := string(planBody)
		updateOpt := *opt
		response, err := callWithRetry(ctx, updateRetry, func() (*resty.Response, error) {
			return c.Update(ctx, updatePath, updateBody, updateOpt)
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"calling update API",
				err.Error(),
			)
			return
		}
		if !response.IsSuccess() {
			resp.Diagnostics.AddError(
				fmt.Sprintf("Update API returns %d", response.StatusCode()),
				apiErrorDetail(response),
			)
			return
		}

		// For LRO, wait for completion
		if !plan.PollUpdate.IsNull() {
			var d pollData
			if diags := plan.PollUpdate.As(ctx, &d, basetypes.ObjectAsOptions{}); diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}

			opt, diags := r.p.apiOpt.ForPoll(ctx, opt.Header, opt.Query, d, r.getOutput(state))
			if diags.HasError() {
				resp.Diagnostics.Append(diags...)
				return
			}
			p, err := client.NewPollableForPoll(*response, *opt)
			if err != nil {
				resp.Diagnostics.AddError(
					"Update: Failed to build poller from the response of the initiated request",
					err.Error(),
				)
				return
			}
			if err := p.PollUntilDone(ctx, c, nil); err != nil {
				resp.Diagnostics.AddError(
					"Update: Polling failure",
					err.Error(),
				)
				return
			}
		}
	}

	// Temporarily set the output here, so that the Read at the end can
	// expand the `$(body)` parameters.
	if plan.UseSensitiveOutput.ValueBool() {
		plan.SensitiveOutput = state.SensitiveOutput
		plan.Output = types.DynamicNull()
	} else {
		plan.Output = state.Output
		plan.SensitiveOutput = types.DynamicNull()
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	// Persist auth_ref to private state for future destroy operations.
	if !plan.AuthRef.IsNull() {
		authRefJSON, _ := json.Marshal(plan.AuthRef.ValueString())
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, "auth_ref", authRefJSON)...)
	}

	diags = ephemeral.Set(ctx, resp.Private, eb)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	rreq := resource.ReadRequest{
		State:        resp.State,
		Private:      resp.Private,
		ProviderMeta: req.ProviderMeta,
		Identity:     req.Identity,
	}
	rresp := resource.ReadResponse{
		State:       resp.State,
		Diagnostics: resp.Diagnostics,
		Identity:    resp.Identity,
	}
	r.read(ctx, rreq, &rresp, false)

	resp.State = rresp.State
	resp.Diagnostics = rresp.Diagnostics
	resp.Identity = rresp.Identity
}

func (r Resource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state resourceData
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	authRef := state.AuthRef.ValueString()
	if authRef == "" {
		// Fallback: check private state for auth_ref persisted by ModifyPlan.
		// Handles pre-existing resources whose state was written before auth_ref existed.
		if raw, diags := req.Private.GetKey(ctx, "auth_ref"); !diags.HasError() && len(raw) > 0 {
			_ = json.Unmarshal(raw, &authRef)
		}
	}
	c, err := r.p.ClientForAuthRef(authRef)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resolve auth_ref", err.Error())
		return
	}
	c.SetLoggerContext(ctx)

	tflog.Info(ctx, "Delete a resource", map[string]any{"id": state.ID.ValueString()})

	stateOutput, err := dynamic.ToJSON(r.getOutput(state))
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to marshal json for `output`",
			err.Error(),
		)
		return
	}

	opt, diags := r.p.apiOpt.ForResourceDelete(ctx, state, stateOutput)
	resp.Diagnostics.Append(diags...)
	if diags.HasError() {
		return
	}

	// Precheck
	if !state.PrecheckDelete.IsNull() {
		unlockFunc, diags := precheck(ctx, c, r.p.apiOpt, state.ID.ValueString(), opt.Header, opt.Query, state.PrecheckDelete, r.getOutput(state))
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		defer unlockFunc()
	}

	path := state.ID.ValueString()
	// Overwrite the path with delete_path, if set.
	if !state.DeletePath.IsNull() {
		path, err = exparam.ExpandBodyOrPath(state.DeletePath.ValueString(), state.Path.ValueString(), stateOutput)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to build the path for deleting the resource",
				fmt.Sprintf("Can't build path with `delete_path`: %q, `path`: %q, `body`: %q", state.DeletePath.ValueString(), state.Path.ValueString(), stateOutput),
			)
			return
		}
	}

	var body string
	if db := state.DeleteBody; !db.IsNull() {
		b, err := dynamic.ToJSON(db)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to marshal `delete_body`",
				err.Error(),
			)
		}
		body = string(b)
	}
	if db := state.DeleteBodyRaw; !db.IsNull() {
		b, err := exparam.ExpandBody(db.ValueString(), stateOutput)
		if err != nil {
			resp.Diagnostics.AddError(
				"Failed to expand the expressions in the `delete_body_raw`",
				err.Error(),
			)
		}
		body = b
	}

	var deleteRetry *resourceRetryData
	if !state.Retry.IsNull() && !state.Retry.IsUnknown() {
		var rd resourceRetryData
		if diags := state.Retry.As(ctx, &rd, basetypes.ObjectAsOptions{}); !diags.HasError() {
			deleteRetry = &rd
		}
	}
	deletePath := path
	deleteBody := body
	deleteOpt := *opt
	response, err := callWithRetry(ctx, deleteRetry, func() (*resty.Response, error) {
		return c.Delete(ctx, deletePath, deleteBody, deleteOpt)
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"calling delete API",
			err.Error(),
		)
		return
	}

	if strings.EqualFold(opt.Method, "DELETE") {
		if response.StatusCode() == http.StatusNotFound {
			return
		}
	}

	// When poll_delete is configured with status_locator = "code" and the
	// DELETE response code matches a pending status, skip the error and
	// fall through to polling (e.g. 400 ServerIsBusy, 409 Conflict).
	if !response.IsSuccess() {
		retryable := false
		if !state.PollDelete.IsNull() {
			var d pollData
			if diags := state.PollDelete.As(ctx, &d, basetypes.ObjectAsOptions{}); !diags.HasError() {
				pollOpt, diags := r.p.apiOpt.ForPoll(ctx, opt.Header, opt.Query, d, r.getOutput(state))
				if !diags.HasError() {
					if _, ok := pollOpt.StatusLocator.(client.CodeLocator); ok {
						code := fmt.Sprintf("%d", response.StatusCode())
						for _, ps := range pollOpt.Status.Pending {
							if strings.EqualFold(code, ps) {
								retryable = true
								break
							}
						}
					}
				}
			}
		}
		if !retryable {
			resp.Diagnostics.AddError(
				fmt.Sprintf("Delete API returns %d", response.StatusCode()),
				apiErrorDetail(response),
			)
			return
		}
	}

	// For LRO, wait for completion
	if !state.PollDelete.IsNull() {
		var d pollData
		if diags := state.PollDelete.As(ctx, &d, basetypes.ObjectAsOptions{}); diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		opt, diags := r.p.apiOpt.ForPoll(ctx, opt.Header, opt.Query, d, r.getOutput(state))
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
		p, err := client.NewPollableForPoll(*response, *opt)
		if err != nil {
			resp.Diagnostics.AddError(
				"Delete: Failed to build poller from the response of the initiated request",
				err.Error(),
			)
			return
		}
		if err := p.PollUntilDone(ctx, c, nil); err != nil {
			resp.Diagnostics.AddError(
				"Delete: Polling failure",
				err.Error(),
			)
			return
		}
	}
}

type ImportSpec struct {
	// Id is the resource id. Required.
	Id string `json:"id"`

	// Path is the path used to create the resource. Required.
	Path string `json:"path"`

	// Query is only required when it is mandatory for reading the resource.
	Query url.Values `json:"query,omitempty"`

	// Header is only required when it is mandatory for reading the resource.
	Header map[string]string `json:"header,omitempty"`

	// Body represents the properties expected to be managed and tracked by Terraform. The value of these properties can be null as a place holder.
	// When absent, all the response payload read wil be set to `body`.
	Body *json.RawMessage `json:"body,omitempty"`

	// ReadSelector is only required when reading the ID returns a list of resources, and you'd like to read only one of them.
	// Note that in this case, the value of the `Body` is likely required if the selector reference the body.
	ReadSelector *string `json:"read_selector,omitempty"`

	// ReadResponseTemplate is only required when the response from read is structually different than the `body`.
	ReadResponseTemplate *string `json:"read_response_template,omitempty"`
}

func (r *Resource) IdentitySchema(ctx context.Context, req resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				Description:       "The import format described above.",
				RequiredForImport: true,
			},
		},
	}
}

func (Resource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idPath := tfpath.Root("id")
	pathPath := tfpath.Root("path")
	queryPath := tfpath.Root("query")
	headerPath := tfpath.Root("header")
	bodyPath := tfpath.Root("body")
	readSelector := tfpath.Root("read_selector")
	readResponseTemplate := tfpath.Root("read_response_template")

	var (
		imp ImportSpec
		err error
	)
	if req.ID != "" {
		err = json.Unmarshal([]byte(req.ID), &imp)

		// Ensure the identity is set and populated to the response
		resp.Identity.SetAttribute(ctx, idPath, req.ID)
	} else {
		var identity types.String
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, idPath, &identity)...)
		if resp.Diagnostics.HasError() {
			return
		}
		err = json.Unmarshal([]byte(identity.ValueString()), &imp)
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Resource Import Error",
			fmt.Sprintf("failed to unmarshal ID: %v", err),
		)
		return
	}

	if imp.Id == "" {
		resp.Diagnostics.AddError(
			"Resource Import Error",
			"`id` not specified in the import spec",
		)
		return
	}

	if imp.Path == "" {
		resp.Diagnostics.AddError(
			"Resource Import Error",
			"`path` not specified in the import spec",
		)
		return
	}

	// Set the state to passthrough to the read
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, idPath, imp.Id)...)

	if imp.Body != nil {
		body, err := dynamic.FromJSONImplied(*imp.Body)
		if err != nil {
			resp.Diagnostics.AddError(
				"Resource Import Error",
				fmt.Sprintf("unmarshal `body`: %v", err),
			)
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, bodyPath, body)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathPath, imp.Path)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, queryPath, imp.Query)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, headerPath, imp.Header)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, readSelector, imp.ReadSelector)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, readResponseTemplate, imp.ReadResponseTemplate)...)
}

func (r *Resource) RenderOption() tffwdocs.ResourceRenderOption {
	return tffwdocs.ResourceRenderOption{
		Examples: []tffwdocs.Example{
			{
				Header: "Azure Resource Group",
				HCL: `
resource "rest_resource" "rg" {
  path = format("/subscriptions/%s/resourceGroups/%s", var.subscription_id, "example")
  query = {
    api-version = ["2020-06-01"]
  }
  create_method = "PUT"
  poll_delete = {
    status_locator = "code"
    status = {
      success = ["404"]
      pending = ["202", "200"]
    }
  }
  body = {
    location = "westus"
    tags = {
      foo = "bar"
    }
  }
}
					`,
			},
		},
		ImportId: &tffwdocs.ImportId{
			Format: `
- id (Required)                        : The resource id.
- path (Required)                      : The path used to create the resource (as this is force new)
- query (Optional)                     : The query parameters.
- header (Optional)                    : The header.
- body (Optional)                      : The interested properties in the response body that you want to manage via this resource.
                                         If you omit this, then all the properties will be keeping track, which in most cases is 
                                         not what you want (e.g. the read only attributes shouldn't be managed).
                                         The value of each property is not important here, hence leave them as "null".
- read_selector (Optional)             : The read_selector used to specify the resource from a collection of resources.
- read_response_template (Optional)    : The read_response_template used to transform the structure of the read response.
`,
			ExampleId: `{
  \"id\": \"/subscriptions/0-0-0-0/resourceGroups/example\",
  \"path\": \"/subscriptions/0-0-0-0/resourceGroups/example\",
  \"query\": {\"api-version\": [\"2020-06-01\"]},
  \"body\": {
    \"location\": null,
    \"tags\": null
  }
}`,
			ExampleBlk: `
import {
  to = rest_resource.test
  id = jsonencode({
    id = "/posts/1"
    path = "/posts"
    body = {
      foo = null
    }
    header = {
      key = "val"
    }
    query = {
      x = ["y"]
    }
  })
}`,
		},
		IdentityExamples: []tffwdocs.Example{
			{
				HCL: `
id = jsonencode({
  id = "/posts/1"
  path = "/posts"
  body = {
	foo = null
  }
  header = {
	key = "val"
  }
  query = {
	x = ["y"]
  }
})
`,
			},
		},
	}
}
