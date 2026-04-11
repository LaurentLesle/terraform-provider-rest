package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModifyJSON(t *testing.T) {
	cases := []struct {
		name           string
		base           string
		body           string
		writeOnlyAttrs []string
		expect         any
	}{
		{
			name:   "invalid base",
			base:   "",
			expect: errors.New(`unmarshal the base "": unexpected end of JSON input`),
		},
		{
			name:   "invalid body",
			base:   "{}",
			body:   "",
			expect: errors.New(`unmarshal the body "": unexpected end of JSON input`),
		},
		{
			name:           "with write_only_attrs",
			base:           `{"obj":{"a":1,"b":2}, "z":2}`,
			body:           `{"obj":{"a":3,"d":"5"}, "new":4}`,
			writeOnlyAttrs: []string{"obj.b"},
			expect:         `{"obj":{"a":3,"b":2}}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ModifyBody(tt.base, tt.body, tt.writeOnlyAttrs)
			switch expect := tt.expect.(type) {
			case error:
				require.EqualError(t, err, expect.Error())
			default:
				require.NoError(t, err)
				require.Equal(t, expect, actual)
			}
		})
	}
}

func TestGetUpdatedJSON(t *testing.T) {
	cases := []struct {
		name    string
		oldJSON any
		newJSON any
		expect  any
	}{
		{
			name:    "simple object",
			oldJSON: map[string]any{"a": 1, "b": 2},
			newJSON: map[string]any{"a": 3, "c": 4},
			expect:  map[string]any{"a": 3},
		},
		{
			name:    "nested object",
			oldJSON: map[string]any{"obj": map[string]any{"a": 1, "b": 2, "c": 3}, "z": 2},
			newJSON: map[string]any{"obj": map[string]any{"a": 3, "b": 4, "d": 5}, "new": 4},
			expect:  map[string]any{"obj": map[string]any{"a": 3, "b": 4}},
		},
		{
			name:    "simple array",
			oldJSON: []any{1, 2, 3},
			newJSON: []any{3, 4, 5},
			expect:  []any{3, 4, 5},
		},
		{
			name:    "simple array with different size",
			oldJSON: []any{1, 2, 3},
			newJSON: []any{3},
			expect:  []any{3},
		},
		{
			name: "complex array",
			oldJSON: []any{
				map[string]any{
					"a": 1,
					"b": 2,
				},
				map[string]any{
					"a": 1,
					"b": 2,
				},
			},
			newJSON: []any{
				map[string]any{
					"a": 1,
					"c": 3,
				},
				map[string]any{
					"b": 2,
					"c": 3,
				},
			},
			expect: []any{
				map[string]any{
					"a": 1,
				},
				map[string]any{
					"b": 2,
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual := getUpdatedJSON(tt.oldJSON, tt.newJSON)
			require.Equal(t, tt.expect, actual)
		})
	}
}

func TestGetUpdatedJSONForImport(t *testing.T) {
	cases := []struct {
		name    string
		oldJSON any
		newJSON any
		expect  any
	}{
		{
			name:    "nil",
			oldJSON: nil,
			newJSON: map[string]any{"a": 1},
			expect:  map[string]any{"a": 1},
		},
		{
			name:    "simple object",
			oldJSON: map[string]any{"a": nil, "b": nil},
			newJSON: map[string]any{"a": 3, "c": 4},
			expect:  map[string]any{"a": 3},
		},
		{
			name:    "nested object",
			oldJSON: map[string]any{"obj": map[string]any{"a": nil, "b": nil, "c": nil}, "z": nil},
			newJSON: map[string]any{"obj": map[string]any{"a": 3, "b": 4, "d": 5}, "new": 4},
			expect:  map[string]any{"obj": map[string]any{"a": 3, "b": 4}},
		},
		{
			name:    "nested object with no child detail",
			oldJSON: map[string]any{"obj": nil, "z": nil},
			newJSON: map[string]any{"obj": map[string]any{"a": 3, "b": 4, "d": 5}, "new": 4},
			expect:  map[string]any{"obj": map[string]any{"a": 3, "b": 4, "d": 5}},
		},
		{
			name:    "0 sized array is the same as nil",
			oldJSON: []any{},
			newJSON: []any{1, 2, 3},
			expect:  []any{1, 2, 3},
		},
		{
			name:    "0 sized array is also the same as of a single nil element",
			oldJSON: []any{nil},
			newJSON: []any{1, 2, 3},
			expect:  []any{1, 2, 3},
		},
		// TODO
		{
			name:    "more than one element in array",
			oldJSON: []any{nil, nil},
			newJSON: []any{1, 2, 3},
			expect:  errors.New("the length of array should be 1"),
		},
		{
			name: "complex array",
			oldJSON: []any{
				map[string]any{
					"a": 1,
					"b": 2,
				},
			},
			newJSON: []any{
				map[string]any{
					"a": 1,
					"c": 3,
				},
				map[string]any{
					"b": 2,
					"c": 3,
				},
			},
			expect: []any{
				map[string]any{
					"a": 1,
				},
				map[string]any{
					"b": 2,
				},
			},
		},
		{
			name: "object nesting complex array",
			oldJSON: map[string]any{
				"prop": map[string]any{
					"foos": []any{
						map[string]any{
							"a": nil,
						},
					},
				},
			},
			newJSON: map[string]any{
				"prop": map[string]any{
					"foos": []any{
						map[string]any{
							"a": 0,
							"b": 0,
						},
						map[string]any{
							"a": 0,
							"b": 0,
						},
					},
					"bar": 0,
				},
			},
			expect: map[string]any{
				"prop": map[string]any{
					"foos": []any{
						map[string]any{
							"a": 0,
						},
						map[string]any{
							"a": 0,
						},
					},
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := getUpdatedJSONForImport(tt.oldJSON, tt.newJSON)
			switch expect := tt.expect.(type) {
			case error:
				require.EqualError(t, err, expect.Error())
			default:
				require.NoError(t, err)
				require.Equal(t, expect, actual)
			}
		})
	}
}

func TestFilterJSON(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		outputAttrs []string
		expect      string
		expectError bool
	}{
		{
			name:        "invalid body",
			body:        "",
			outputAttrs: []string{"foo"},
			expectError: true,
		},
		{
			name:        "object with non existed attrs",
			body:        `{}`,
			outputAttrs: []string{"foo"},
			expect:      `{}`,
		},
		{
			name:        "object with splat addr",
			body:        `{}`,
			outputAttrs: []string{"#"},
			expectError: true,
		},
		{
			name:        "array with non existed attrs",
			body:        `[]`,
			outputAttrs: []string{"#.foo"},
			expect:      `[]`,
		},
		{
			name:        "array with key addr",
			body:        `[]`,
			outputAttrs: []string{"foo"},
			expectError: true,
		},
		{
			name:        "filter object",
			body:        `{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}}`,
			outputAttrs: []string{"a", "obj.x"},
			expect:      `{"a": 1, "obj": {"x": 1}}`,
		},
		{
			name:        "filter object for nothing",
			body:        `{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}}`,
			outputAttrs: []string{},
			expect:      `{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}}`,
		},
		{
			name:        "filter array",
			body:        `[{"a": 1}, {"b": 2}, {"obj": {"x": 1, "y": 2}}]`,
			outputAttrs: []string{"#.a", "#.obj.x"},
			expect:      `[{"a": 1}, {}, {"obj": {"x": 1}}]`,
		},
		{
			name: "filter array of same elements",
			body: `[
				{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}},
				{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}},
				{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}}
			]`,
			outputAttrs: []string{"#.a", "#.obj.x"},
			expect: `[
				{"a": 1, "obj": {"x": 1}},
				{"a": 1, "obj": {"x": 1}},
				{"a": 1, "obj": {"x": 1}}
			]`,
		},
		{
			name: "filter array nested in array",
			body: `[
				[ 
					{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}},
					{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}}
				],
				[ 
					{"a": 1, "b": 2, "obj": {"x": 1, "y": 2}}
				],
				[]
			]`,
			outputAttrs: []string{"#.#.a", "#.#.obj.x"},
			expect: `[
				[
					{"a": 1, "obj": {"x": 1}},
					{"a": 1, "obj": {"x": 1}}
				],
				[
					{"a": 1, "obj": {"x": 1}}
				],
				[]
			]`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := FilterAttrsInJSON(tt.body, tt.outputAttrs)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tt.expect, actual)
		})
	}
}
