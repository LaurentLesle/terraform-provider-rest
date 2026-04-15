package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyIgnoreBodyChanges(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		stateBody   string
		ignorePaths []string
		wantBody    string
		wantErr     bool
	}{
		{
			name:        "no paths — identity",
			body:        `{"a":1,"b":2}`,
			stateBody:   `{"a":99,"b":2}`,
			ignorePaths: nil,
			wantBody:    `{"a":1,"b":2}`,
		},
		{
			name:        "path present in state — restored",
			body:        `{"a":1,"b":"api-value"}`,
			stateBody:   `{"a":1,"b":"state-value"}`,
			ignorePaths: []string{"b"},
			wantBody:    `{"a":1,"b":"state-value"}`,
		},
		{
			name:        "path absent from state — deleted from body",
			body:        `{"a":1,"b":"api-value"}`,
			stateBody:   `{"a":1}`,
			ignorePaths: []string{"b"},
			wantBody:    `{"a":1}`,
		},
		{
			name:        "nested path",
			body:        `{"props":{"tier":"Premium"}}`,
			stateBody:   `{"props":{"tier":"Standard"}}`,
			ignorePaths: []string{"props.tier"},
			wantBody:    `{"props":{"tier":"Standard"}}`,
		},
		{
			name:        "multiple paths",
			body:        `{"a":"api-a","b":"api-b","c":"api-c"}`,
			stateBody:   `{"a":"state-a","c":"state-c"}`,
			ignorePaths: []string{"a", "b", "c"},
			wantBody:    `{"a":"state-a","c":"state-c"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyIgnoreBodyChanges([]byte(tc.body), []byte(tc.stateBody), tc.ignorePaths)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			var gotMap, wantMap map[string]interface{}
			require.NoError(t, json.Unmarshal(got, &gotMap))
			require.NoError(t, json.Unmarshal([]byte(tc.wantBody), &wantMap))
			require.Equal(t, wantMap, gotMap)
		})
	}
}

func TestNormalizeBodyCasing(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		stateBody string
		wantBody  string
	}{
		{
			name:      "no diff — unchanged",
			body:      `{"a":"Standard"}`,
			stateBody: `{"a":"Standard"}`,
			wantBody:  `{"a":"Standard"}`,
		},
		{
			name:      "case diff — state casing wins",
			body:      `{"a":"Standard"}`,
			stateBody: `{"a":"standard"}`,
			wantBody:  `{"a":"standard"}`,
		},
		{
			name:      "actual diff — body value preserved",
			body:      `{"a":"Premium"}`,
			stateBody: `{"a":"Standard"}`,
			wantBody:  `{"a":"Premium"}`,
		},
		{
			name:      "nested object",
			body:      `{"props":{"sku":"PREMIUM"}}`,
			stateBody: `{"props":{"sku":"premium"}}`,
			wantBody:  `{"props":{"sku":"premium"}}`,
		},
		{
			name:      "array of strings",
			body:      `["Standard","Premium"]`,
			stateBody: `["standard","Premium"]`,
			wantBody:  `["standard","Premium"]`,
		},
		{
			name:      "key absent from state — left alone",
			body:      `{"a":"Standard","b":"Extra"}`,
			stateBody: `{"a":"standard"}`,
			wantBody:  `{"a":"standard","b":"Extra"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeBodyCasing([]byte(tc.body), []byte(tc.stateBody))

			var gotV, wantV interface{}
			require.NoError(t, json.Unmarshal(got, &gotV))
			require.NoError(t, json.Unmarshal([]byte(tc.wantBody), &wantV))
			require.Equal(t, wantV, gotV)
		})
	}
}
