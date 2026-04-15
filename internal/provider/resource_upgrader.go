package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/magodo/terraform-plugin-framework-helper/dynamic"
	"github.com/LaurentLesle/terraform-provider-rest/internal/provider/migrate"
)

// v4StatusAttrTypes is the shape of status in V4 state (success = list of strings).
var v4StatusAttrTypes = map[string]attr.Type{
	"success": types.ListType{ElemType: types.StringType},
	"pending": types.ListType{ElemType: types.StringType},
}

// v4PollAttrTypes is the full attribute type map for a V4 poll object.
var v4PollAttrTypes = map[string]attr.Type{
	"status_locator":    types.StringType,
	"status":            types.ObjectType{AttrTypes: v4StatusAttrTypes},
	"url_locator":       types.StringType,
	"header":            types.MapType{ElemType: types.StringType},
	"default_delay_sec": types.Int64Type,
}

// v4PrecheckApiAttrTypes is the attribute type map for the V4 precheck api object.
var v4PrecheckApiAttrTypes = map[string]attr.Type{
	"status_locator":    types.StringType,
	"status":            types.ObjectType{AttrTypes: v4StatusAttrTypes},
	"path":              types.StringType,
	"query":             types.MapType{ElemType: types.ListType{ElemType: types.StringType}},
	"header":            types.MapType{ElemType: types.StringType},
	"default_delay_sec": types.Int64Type,
}

// upgradePollV3toV4 converts a V3 poll object (success=string) to a V4 poll
// object (success=[]string). Null/unknown objects pass through unchanged.
func upgradePollV3toV4(ctx context.Context, poll types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if poll.IsNull() || poll.IsUnknown() {
		return types.ObjectNull(v4PollAttrTypes), diags
	}

	// Decode V3 poll using V3 attr types.
	type v3PollDecode struct {
		StatusLocator types.String `tfsdk:"status_locator"`
		Status        types.Object `tfsdk:"status"`
		UrlLocator    types.String `tfsdk:"url_locator"`
		Header        types.Map    `tfsdk:"header"`
		DefaultDelay  types.Int64  `tfsdk:"default_delay_sec"`
	}
	var pd v3PollDecode
	diags.Append(poll.As(ctx, &pd, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return types.ObjectNull(v4PollAttrTypes), diags
	}

	newStatus, d := upgradeStatusV3toV4(ctx, pd.Status)
	diags.Append(d...)
	if diags.HasError() {
		return types.ObjectNull(v4PollAttrTypes), diags
	}

	newPoll, d := types.ObjectValue(v4PollAttrTypes, map[string]attr.Value{
		"status_locator":    pd.StatusLocator,
		"status":            newStatus,
		"url_locator":       pd.UrlLocator,
		"header":            pd.Header,
		"default_delay_sec": pd.DefaultDelay,
	})
	diags.Append(d...)
	return newPoll, diags
}

// upgradeStatusV3toV4 converts a V3 status object (success=string) to a V4
// status object (success=[]string).
func upgradeStatusV3toV4(ctx context.Context, status types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if status.IsNull() || status.IsUnknown() {
		return types.ObjectNull(v4StatusAttrTypes), diags
	}

	type v3StatusDecode struct {
		Success types.String `tfsdk:"success"`
		Pending types.List   `tfsdk:"pending"`
	}
	var sd v3StatusDecode
	diags.Append(status.As(ctx, &sd, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return types.ObjectNull(v4StatusAttrTypes), diags
	}

	var successElems []attr.Value
	if !sd.Success.IsNull() && !sd.Success.IsUnknown() {
		successElems = []attr.Value{sd.Success}
	}
	successList, d := types.ListValue(types.StringType, successElems)
	diags.Append(d...)
	if diags.HasError() {
		return types.ObjectNull(v4StatusAttrTypes), diags
	}

	newStatus, d := types.ObjectValue(v4StatusAttrTypes, map[string]attr.Value{
		"success": successList,
		"pending": sd.Pending,
	})
	diags.Append(d...)
	return newStatus, diags
}

// upgradePrecheckV3toV4 converts a V3 precheck list (api.status.success=string)
// to a V4 precheck list (api.status.success=[]string).
func upgradePrecheckV3toV4(ctx context.Context, precheck types.List) (types.List, diag.Diagnostics) {
	var diags diag.Diagnostics
	if precheck.IsNull() || precheck.IsUnknown() {
		return precheck, diags
	}

	v4PrecheckItemAttrTypes := map[string]attr.Type{
		"mutex": types.StringType,
		"api":   types.ObjectType{AttrTypes: v4PrecheckApiAttrTypes},
	}

	type v3PrecheckItemDecode struct {
		Mutex types.String `tfsdk:"mutex"`
		Api   types.Object `tfsdk:"api"`
	}
	type v3PrecheckApiDecode struct {
		StatusLocator types.String `tfsdk:"status_locator"`
		Status        types.Object `tfsdk:"status"`
		Path          types.String `tfsdk:"path"`
		Query         types.Map    `tfsdk:"query"`
		Header        types.Map    `tfsdk:"header"`
		DefaultDelay  types.Int64  `tfsdk:"default_delay_sec"`
	}

	var items []v3PrecheckItemDecode
	diags.Append(precheck.ElementsAs(ctx, &items, false)...)
	if diags.HasError() {
		return precheck, diags
	}

	newItems := make([]attr.Value, 0, len(items))
	for _, item := range items {
		newApi := item.Api
		if !item.Api.IsNull() && !item.Api.IsUnknown() {
			var api v3PrecheckApiDecode
			diags.Append(item.Api.As(ctx, &api, basetypes.ObjectAsOptions{})...)
			if diags.HasError() {
				return precheck, diags
			}

			newStatus, d := upgradeStatusV3toV4(ctx, api.Status)
			diags.Append(d...)
			if diags.HasError() {
				return precheck, diags
			}

			newApi, d = types.ObjectValue(v4PrecheckApiAttrTypes, map[string]attr.Value{
				"status_locator":    api.StatusLocator,
				"status":            newStatus,
				"path":              api.Path,
				"query":             api.Query,
				"header":            api.Header,
				"default_delay_sec": api.DefaultDelay,
			})
			diags.Append(d...)
			if diags.HasError() {
				return precheck, diags
			}
		}

		newItem, d := types.ObjectValue(v4PrecheckItemAttrTypes, map[string]attr.Value{
			"mutex": item.Mutex,
			"api":   newApi,
		})
		diags.Append(d...)
		if diags.HasError() {
			return precheck, diags
		}
		newItems = append(newItems, newItem)
	}

	newList, d := types.ListValue(types.ObjectType{AttrTypes: v4PrecheckItemAttrTypes}, newItems)
	diags.Append(d...)
	return newList, diags
}

func (r *Resource) UpgradeState(context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema: &migrate.ResourceSchemaV0,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var pd migrate.ResourceDataV0

				resp.Diagnostics.Append(req.State.Get(ctx, &pd)...)

				if resp.Diagnostics.HasError() {
					return
				}

				var err error

				body := types.DynamicNull()
				if !pd.Body.IsNull() {
					body, err = dynamic.FromJSONImplied([]byte(pd.Body.ValueString()))
					if err != nil {
						resp.Diagnostics.AddError(
							"Upgrade State Error",
							fmt.Sprintf(`Converting "body": %v`, err),
						)
					}
				}

				output := types.DynamicNull()
				if !output.IsNull() {
					output, err = dynamic.FromJSONImplied([]byte(pd.Output.ValueString()))
					if err != nil {
						resp.Diagnostics.AddError(
							"Upgrade State Error",
							fmt.Sprintf(`Converting "output": %v`, err),
						)
					}
				}

				upgradedStateData := migrate.ResourceDataV1{
					ID:                  pd.ID,
					Path:                pd.Path,
					CreateSelector:      pd.CreateSelector,
					ReadSelector:        pd.ReadSelector,
					ReadPath:            pd.ReadPath,
					UpdatePath:          pd.UpdatePath,
					DeletePath:          pd.DeletePath,
					CreateMethod:        pd.CreateMethod,
					UpdateMethod:        pd.UpdateMethod,
					DeleteMethod:        pd.DeleteMethod,
					PrecheckCreate:      pd.PrecheckCreate,
					PrecheckUpdate:      pd.PrecheckUpdate,
					PrecheckDelete:      pd.PrecheckDelete,
					Body:                body,
					PollCreate:          pd.PollCreate,
					PollUpdate:          pd.PollUpdate,
					PollDelete:          pd.PollDelete,
					RetryCreate:         pd.RetryCreate,
					RetryRead:           pd.RetryRead,
					RetryUpdate:         pd.RetryUpdate,
					RetryDelete:         pd.RetryDelete,
					WriteOnlyAttributes: pd.WriteOnlyAttributes,
					MergePatchDisabled:  pd.MergePatchDisabled,
					Query:               pd.Query,
					Header:              pd.Header,
					CheckExistance:      pd.CheckExistance,
					ForceNewAttrs:       pd.ForceNewAttrs,
					OutputAttrs:         pd.OutputAttrs,
					Output:              output,
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, upgradedStateData)...)
			},
		},
		1: {
			PriorSchema: &migrate.ResourceSchemaV1,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var pd migrate.ResourceDataV1

				resp.Diagnostics.Append(req.State.Get(ctx, &pd)...)

				if resp.Diagnostics.HasError() {
					return
				}

				// V1 poll/precheck also had success as a single string.
				// Convert to []string for V4 schema compatibility.
				pollCreate, d := upgradePollV3toV4(ctx, pd.PollCreate)
				resp.Diagnostics.Append(d...)
				pollUpdate, d := upgradePollV3toV4(ctx, pd.PollUpdate)
				resp.Diagnostics.Append(d...)
				pollDelete, d := upgradePollV3toV4(ctx, pd.PollDelete)
				resp.Diagnostics.Append(d...)
				precheckCreate, d := upgradePrecheckV3toV4(ctx, pd.PrecheckCreate)
				resp.Diagnostics.Append(d...)
				precheckUpdate, d := upgradePrecheckV3toV4(ctx, pd.PrecheckUpdate)
				resp.Diagnostics.Append(d...)
				precheckDelete, d := upgradePrecheckV3toV4(ctx, pd.PrecheckDelete)
				resp.Diagnostics.Append(d...)
				if resp.Diagnostics.HasError() {
					return
				}

				upgradedStateData := resourceData{
					ID:                  pd.ID,
					Path:                pd.Path,
					CreateSelector:      pd.CreateSelector,
					ReadSelector:        pd.ReadSelector,
					ReadPath:            pd.ReadPath,
					UpdatePath:          pd.UpdatePath,
					DeletePath:          pd.DeletePath,
					CreateMethod:        pd.CreateMethod,
					UpdateMethod:        pd.UpdateMethod,
					DeleteMethod:        pd.DeleteMethod,
					PrecheckCreate:      precheckCreate,
					PrecheckUpdate:      precheckUpdate,
					PrecheckDelete:      precheckDelete,
					Body:                pd.Body,
					PollCreate:          pollCreate,
					PollUpdate:          pollUpdate,
					PollDelete:          pollDelete,
					WriteOnlyAttributes: pd.WriteOnlyAttributes,
					MergePatchDisabled:  pd.MergePatchDisabled,
					Query:               pd.Query,
					Header:              pd.Header,
					CheckExistance:      pd.CheckExistance,
					ForceNewAttrs:       pd.ForceNewAttrs,
					OutputAttrs:         pd.OutputAttrs,
					Output:              pd.Output,
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, upgradedStateData)...)
			},
		},
		2: {
			PriorSchema: &migrate.ResourceSchemaV2,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var pd migrate.ResourceDataV2

				resp.Diagnostics.Append(req.State.Get(ctx, &pd)...)
				if resp.Diagnostics.HasError() {
					return
				}

				// V2 state already stored success as a list (same as V4), so no
				// string→list conversion is needed for poll/precheck.
				// V2's `retry` block (status_locator/count/wait_in_sec) is incompatible
				// with V4's regex-based retry — intentionally dropped.
				// `body_value_case_insensitive` and `ignore_body_changes` have the
				// same types in V2 and V4 so they are carried forward.
				upgradedStateData := resourceData{
					ID:   pd.ID,
					Path: pd.Path,

					AuthRef: pd.AuthRef,

					BodyValueCaseInsensitive: pd.BodyValueCaseInsensitive,
					IgnoreBodyChanges:        pd.IgnoreBodyChanges,

					CreateSelector:       pd.CreateSelector,
					ReadSelector:         pd.ReadSelector,
					ReadResponseTemplate: pd.ReadResponseTemplate,

					ReadPath:   pd.ReadPath,
					UpdatePath: pd.UpdatePath,
					DeletePath: pd.DeletePath,

					CreateMethod: pd.CreateMethod,
					UpdateMethod: pd.UpdateMethod,
					DeleteMethod: pd.DeleteMethod,

					Body:              pd.Body,
					DeleteBody:        pd.DeleteBody,
					DeleteBodyRaw:     pd.DeleteBodyRaw,
					UpdateBodyPatches: pd.UpdateBodyPatches,

					PollCreate: pd.PollCreate,
					PollUpdate: pd.PollUpdate,
					PollDelete: pd.PollDelete,

					PrecheckCreate: pd.PrecheckCreate,
					PrecheckUpdate: pd.PrecheckUpdate,
					PrecheckDelete: pd.PrecheckDelete,

					PostCreateRead: pd.PostCreateRead,

					WriteOnlyAttributes: pd.WriteOnlyAttributes,
					MergePatchDisabled:  pd.MergePatchDisabled,

					Query:       pd.Query,
					CreateQuery: pd.CreateQuery,
					ReadQuery:   pd.ReadQuery,
					UpdateQuery: pd.UpdateQuery,
					DeleteQuery: pd.DeleteQuery,

					Header:       pd.Header,
					CreateHeader: pd.CreateHeader,
					ReadHeader:   pd.ReadHeader,
					UpdateHeader: pd.UpdateHeader,
					DeleteHeader: pd.DeleteHeader,

					CheckExistance: pd.CheckExistance,
					ForceNewAttrs:  pd.ForceNewAttrs,
					OutputAttrs:    pd.OutputAttrs,

					UseSensitiveOutput: pd.UseSensitiveOutput,
					Output:             pd.Output,
					SensitiveOutput:    pd.SensitiveOutput,
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, upgradedStateData)...)
			},
		},
		3: {
			PriorSchema: &migrate.ResourceSchemaV3,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				var pd migrate.ResourceDataV3

				resp.Diagnostics.Append(req.State.Get(ctx, &pd)...)
				if resp.Diagnostics.HasError() {
					return
				}

				// Convert poll status.success from string → []string.
				pollCreate, d := upgradePollV3toV4(ctx, pd.PollCreate)
				resp.Diagnostics.Append(d...)
				pollUpdate, d := upgradePollV3toV4(ctx, pd.PollUpdate)
				resp.Diagnostics.Append(d...)
				pollDelete, d := upgradePollV3toV4(ctx, pd.PollDelete)
				resp.Diagnostics.Append(d...)
				// Convert precheck api.status.success from string → []string.
				precheckCreate, d := upgradePrecheckV3toV4(ctx, pd.PrecheckCreate)
				resp.Diagnostics.Append(d...)
				precheckUpdate, d := upgradePrecheckV3toV4(ctx, pd.PrecheckUpdate)
				resp.Diagnostics.Append(d...)
				precheckDelete, d := upgradePrecheckV3toV4(ctx, pd.PrecheckDelete)
				resp.Diagnostics.Append(d...)
				if resp.Diagnostics.HasError() {
					return
				}

				// New optional fields (retry, ignore_body_changes, body_value_case_insensitive)
				// are not present in V3 state — they default to null, which is correct for
				// Optional attributes.
				upgradedStateData := resourceData{
					ID:   pd.ID,
					Path: pd.Path,

					AuthRef: pd.AuthRef,

					CreateSelector:       pd.CreateSelector,
					ReadSelector:         pd.ReadSelector,
					ReadResponseTemplate: pd.ReadResponseTemplate,

					ReadPath:   pd.ReadPath,
					UpdatePath: pd.UpdatePath,
					DeletePath: pd.DeletePath,

					CreateMethod: pd.CreateMethod,
					UpdateMethod: pd.UpdateMethod,
					DeleteMethod: pd.DeleteMethod,

					Body:              pd.Body,
					DeleteBody:        pd.DeleteBody,
					DeleteBodyRaw:     pd.DeleteBodyRaw,
					UpdateBodyPatches: pd.UpdateBodyPatches,

					PollCreate: pollCreate,
					PollUpdate: pollUpdate,
					PollDelete: pollDelete,

					PrecheckCreate: precheckCreate,
					PrecheckUpdate: precheckUpdate,
					PrecheckDelete: precheckDelete,

					PostCreateRead: pd.PostCreateRead,

					WriteOnlyAttributes: pd.WriteOnlyAttributes,
					MergePatchDisabled:  pd.MergePatchDisabled,

					Query:       pd.Query,
					CreateQuery: pd.CreateQuery,
					ReadQuery:   pd.ReadQuery,
					UpdateQuery: pd.UpdateQuery,
					DeleteQuery: pd.DeleteQuery,

					Header:       pd.Header,
					CreateHeader: pd.CreateHeader,
					ReadHeader:   pd.ReadHeader,
					UpdateHeader: pd.UpdateHeader,
					DeleteHeader: pd.DeleteHeader,

					CheckExistance: pd.CheckExistance,
					ForceNewAttrs:  pd.ForceNewAttrs,
					OutputAttrs:    pd.OutputAttrs,

					UseSensitiveOutput: pd.UseSensitiveOutput,
					Output:             pd.Output,
					SensitiveOutput:    pd.SensitiveOutput,
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, upgradedStateData)...)
			},
		},
	}
}
