package provider

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/magodo/terraform-plugin-framework-helper/dynamic"
	"github.com/LaurentLesle/terraform-provider-rest/internal/client"
	"github.com/LaurentLesle/terraform-provider-rest/internal/defaults"
)

type apiOption struct {
	BaseURL            url.URL
	CreateMethod       string
	UpdateMethod       string
	DeleteMethod       string
	MergePatchDisabled bool
	Query              client.Query
	Header             client.Header
}

func (opt apiOption) ForResourceCreate(ctx context.Context, d resourceData) (*client.CreateOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	q, dd = q.TakeOrSelf(ctx, d.CreateQuery)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.MergeOrSelf(ctx, d.EphemeralHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.TakeOrSelf(ctx, d.CreateHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	out := client.CreateOption{Method: opt.CreateMethod, Query: q, Header: h}
	if !d.CreateMethod.IsUnknown() && !d.CreateMethod.IsNull() {
		out.Method = d.CreateMethod.ValueString()
	}

	return &out, nil
}

func (opt apiOption) ForResourceRead(ctx context.Context, d resourceData, body []byte) (*client.ReadOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	q, dd = q.TakeWithExparamOrSelf(ctx, d.ReadQuery, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.MergeOrSelf(ctx, d.EphemeralHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.TakeWithExparamOrSelf(ctx, d.ReadHeader, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	return &client.ReadOption{Query: q, Header: h}, nil
}

func (opt apiOption) ForResourcePostCreateRead(ctx context.Context, d resourceData, pr postCreateRead, body []byte) (*client.ReadOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	q, dd = q.TakeWithExparamOrSelf(ctx, pr.Query, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.MergeOrSelf(ctx, d.EphemeralHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.TakeWithExparamOrSelf(ctx, pr.Header, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	return &client.ReadOption{Query: q, Header: h}, nil
}

func (opt apiOption) ForResourceUpdate(ctx context.Context, d resourceData, body []byte) (*client.UpdateOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	q, dd = q.TakeWithExparamOrSelf(ctx, d.UpdateQuery, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.MergeOrSelf(ctx, d.EphemeralHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.TakeWithExparamOrSelf(ctx, d.UpdateHeader, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	out := client.UpdateOption{
		Method:             opt.UpdateMethod,
		MergePatchDisabled: opt.MergePatchDisabled,
		Query:              q,
		Header:             h,
	}
	if !d.UpdateMethod.IsUnknown() && !d.UpdateMethod.IsNull() {
		out.Method = d.UpdateMethod.ValueString()
	}
	if !d.MergePatchDisabled.IsUnknown() && !d.MergePatchDisabled.IsNull() {
		out.MergePatchDisabled = d.MergePatchDisabled.ValueBool()
	}

	return &out, nil
}

func (opt apiOption) ForResourceDelete(ctx context.Context, d resourceData, body []byte) (*client.DeleteOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	q, dd = q.TakeWithExparamOrSelf(ctx, d.DeleteQuery, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.MergeOrSelf(ctx, d.EphemeralHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.TakeWithExparamOrSelf(ctx, d.DeleteHeader, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	out := client.DeleteOption{Method: opt.DeleteMethod, Query: q, Header: h}
	if !d.DeleteMethod.IsUnknown() && !d.DeleteMethod.IsNull() {
		out.Method = d.DeleteMethod.ValueString()
	}

	return &out, nil
}

func (opt apiOption) ForDataSourceRead(ctx context.Context, d dataSourceData) (*client.ReadOptionDS, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	return &client.ReadOptionDS{Method: d.Method.ValueString(), Query: q, Header: h}, nil
}

func (opt apiOption) ForOperation(ctx context.Context, method basetypes.StringValue, defQuery, defHeader basetypes.MapValue, ephemeralHeader basetypes.MapValue, ovQuery, ovHeader basetypes.MapValue, body []byte) (*client.OperationOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, defQuery)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	q, dd = q.TakeWithExparamOrSelf(ctx, ovQuery, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, defHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.MergeOrSelf(ctx, ephemeralHeader)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}
	h, dd = h.TakeWithExparamOrSelf(ctx, ovHeader, body)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	return &client.OperationOption{Method: method.ValueString(), Query: q, Header: h}, nil
}

func (opt apiOption) ForListResourceRead(ctx context.Context, d ListResourceData) (*client.ReadOptionLR, diag.Diagnostics) {
	var diags diag.Diagnostics

	q := opt.Query.Clone()
	q, dd := q.TakeOrSelf(ctx, d.Query)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	h := opt.Header.Clone()
	h, dd = h.TakeOrSelf(ctx, d.Header)
	if diags.Append(dd...); diags.HasError() {
		return nil, diags
	}

	return &client.ReadOptionLR{Method: d.Method.ValueString(), Query: q, Header: h}, nil
}

func (opt apiOption) ForPoll(ctx context.Context, defaultHeader client.Header, defaultQuery client.Query, d pollData, body basetypes.DynamicValue) (*client.PollOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	var status statusDataGo
	if d := d.Status.As(ctx, &status, basetypes.ObjectAsOptions{}); d.HasError() {
		diags.Append(d...)
		return nil, diags
	}

	bodyJSON, err := dynamic.ToJSON(body)
	if err != nil {
		diags.AddError("Failed to convert dynamic body to json", err.Error())
		return nil, diags
	}

	statusLocator, err := expandValueLocator(d.StatusLocator.ValueString(), bodyJSON)
	if err != nil {
		diags.AddError("Failed to parse status locator", err.Error())
		return nil, diags
	}

	var urlLocator client.ValueLocator
	if !d.UrlLocator.IsNull() {
		loc, err := expandValueLocator(d.UrlLocator.ValueString(), bodyJSON)
		if err != nil {
			diags.AddError("Failed to parse url locator", err.Error())
			return nil, diags
		}
		urlLocator = loc
	}

	header := defaultHeader
	if !d.Header.IsNull() {
		var dd diag.Diagnostics
		header, dd = header.Clone().TakeOrSelf(ctx, d.Header)
		if diags.Append(dd...); diags.HasError() {
			return nil, diags
		}
	}

	defaultSec := defaults.PollDefaultDelayInSec
	if !d.DefaultDelay.IsNull() && !d.DefaultDelay.IsUnknown() {
		defaultSec = int(d.DefaultDelay.ValueInt64())
	}

	return &client.PollOption{
		StatusLocator: statusLocator,
		Status: client.PollingStatus{
			Success: status.Success,
			Pending: status.Pending,
		},
		UrlLocator: urlLocator,
		Header:     header,

		// The poll option always use the default query, which is typically is from the original request
		Query: defaultQuery,

		DefaultDelay: time.Duration(defaultSec) * time.Second,
	}, nil
}

func (opt apiOption) ForPrecheck(ctx context.Context, defaultPath string, defaultHeader client.Header, defaultQuery client.Query, d precheckDataApi, body basetypes.DynamicValue) (*client.PollOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	var status statusDataGo
	if d := d.Status.As(ctx, &status, basetypes.ObjectAsOptions{}); d.HasError() {
		diags.Append(d...)
		return nil, diags
	}

	bodyJSON, err := dynamic.ToJSON(body)
	if err != nil {
		diags.AddError("Failed to convert dynamic body to json", err.Error())
		return nil, diags
	}

	statusLocator, err := expandValueLocator(d.StatusLocator.ValueString(), bodyJSON)
	if err != nil {
		diags.AddError("Failed to parse status locator", err.Error())
		return nil, diags
	}

	header := defaultHeader
	if !d.Header.IsNull() {
		if d := d.Header.ElementsAs(ctx, &header, false); d.HasError() {
			diags.Append(d...)
			return nil, diags
		}
	}

	uRL := opt.BaseURL
	path := defaultPath
	if !d.Path.IsNull() {
		path = d.Path.ValueString()
	}
	uRL.Path, err = url.JoinPath(uRL.Path, path)
	if err != nil {
		diags.Append(diag.NewErrorDiagnostic("failed to create precheck option", fmt.Sprintf("joining url: %v", err)))
		return nil, diags
	}

	query := url.Values(defaultQuery)
	if !d.Query.IsNull() {
		var q url.Values
		if d := d.Query.ElementsAs(ctx, &q, false); d.HasError() {
			diags.Append(d...)
			return nil, diags
		}
		query = q
	}
	uRL.RawQuery = query.Encode()
	urlLocator := client.ExactLocator(uRL.String())

	defaultSec := defaults.PrecheckDefaultDelayInSec
	if !d.DefaultDelay.IsNull() && !d.DefaultDelay.IsUnknown() {
		defaultSec = int(d.DefaultDelay.ValueInt64())
	}

	return &client.PollOption{
		StatusLocator: statusLocator,
		Status: client.PollingStatus{
			Success: status.Success,
			Pending: status.Pending,
		},
		UrlLocator:   urlLocator,
		Header:       header,
		DefaultDelay: time.Duration(defaultSec) * time.Second,
	}, nil
}
