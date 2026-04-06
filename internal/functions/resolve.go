package functions

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ function.Function = &ResolveFunction{}

type ResolveFunction struct{}

func NewResolveFunction() function.Function {
	return &ResolveFunction{}
}

func (f *ResolveFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "resolve"
}

func (f *ResolveFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary:     "Resolve a ref: expression against a context map",
		Description: "Parses a 'ref:' prefixed string (e.g. 'ref:resource_groups.test.location|westeurope') and walks the context map using dot-separated path segments. Supports optional default values after '|'. Returns the resolved value as a string.",
		Parameters: []function.Parameter{
			function.DynamicParameter{
				Name:               "context",
				AllowUnknownValues: true,
				Description:        "The resolution context map to walk. Typically built from module outputs and input config merged together.",
			},
			function.StringParameter{
				Name:        "ref",
				Description: "The reference expression. Must start with 'ref:' followed by a dot-separated path. Optionally append '|default_value' for a fallback.",
			},
		},
		Return: function.DynamicReturn{},
	}
}

func (f *ResolveFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var contextVal types.Dynamic
	var ref string

	resp.Error = function.ConcatFuncErrors(
		req.Arguments.Get(ctx, &contextVal, &ref),
	)
	if resp.Error != nil {
		return
	}

	// Strip "ref:" prefix if present
	path := ref
	if strings.HasPrefix(path, "ref:") {
		path = strings.TrimPrefix(path, "ref:")
	}

	// Parse optional default: path|default_value
	var defaultVal *string
	if idx := strings.Index(path, "|"); idx >= 0 {
		d := path[idx+1:]
		defaultVal = &d
		path = path[:idx]
	}

	segments := strings.Split(path, ".")
	if len(segments) == 0 || (len(segments) == 1 && segments[0] == "") {
		resp.Error = function.NewFuncError("ref expression has empty path")
		return
	}

	// Walk the context
	resolved, err := walkDynamic(contextVal.UnderlyingValue(), segments)
	if err != nil {
		if defaultVal != nil {
			resp.Error = function.ConcatFuncErrors(
				resp.Result.Set(ctx, types.DynamicValue(types.StringValue(*defaultVal))),
			)
			return
		}
		resp.Error = function.NewFuncError(fmt.Sprintf("ref:%s - %s", ref, err.Error()))
		return
	}

	resp.Error = function.ConcatFuncErrors(
		resp.Result.Set(ctx, types.DynamicValue(resolved)),
	)
}

// walkDynamic traverses a nested attr.Value using dot-separated path segments.
func walkDynamic(val attr.Value, segments []string) (attr.Value, error) {
	current := val

	for i, seg := range segments {
		if current == nil || current.IsNull() {
			return nil, fmt.Errorf("path segment %q (index %d): value is null", seg, i)
		}
		// If the current value is unknown, we can't walk further but
		// the unknown should propagate so for_each keys stay known.
		if current.IsUnknown() {
			return current, nil
		}

		switch v := current.(type) {
		case types.Object:
			attrs := v.Attributes()
			next, ok := attrs[seg]
			if !ok {
				return nil, fmt.Errorf("path segment %q (index %d): key not found in object (available: %s)", seg, i, joinKeys(attrs))
			}
			current = next

		case types.Map:
			elems := v.Elements()
			next, ok := elems[seg]
			if !ok {
				return nil, fmt.Errorf("path segment %q (index %d): key not found in map (available: %s)", seg, i, joinKeys(elems))
			}
			current = next

		case types.Dynamic:
			return walkDynamic(v.UnderlyingValue(), segments[i:])

		default:
			return nil, fmt.Errorf("path segment %q (index %d): cannot index into %T", seg, i, current)
		}
	}

	return current, nil
}

func joinKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return "<empty>"
	}
	return strings.Join(keys, ", ")
}
