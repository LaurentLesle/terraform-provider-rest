package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyIgnoreBodyChanges replaces the value at each ignored path in body with
// the corresponding value from stateBody (or removes it if absent from state).
func applyIgnoreBodyChanges(body, stateBody []byte, ignorePaths []string) ([]byte, error) {
	pb := string(body)
	for _, p := range ignorePaths {
		var err error
		pb, err = sjson.Delete(pb, p)
		if err != nil {
			return nil, fmt.Errorf("deleting path %q: %w", p, err)
		}
		if sv := gjson.GetBytes(stateBody, p); sv.Exists() {
			pb, err = sjson.SetRaw(pb, p, sv.Raw)
			if err != nil {
				return nil, fmt.Errorf("restoring state value at path %q: %w", p, err)
			}
		}
	}
	return []byte(pb), nil
}

// normalizeBodyCasing walks body and stateBody together. For every string
// value where body and stateBody agree case-insensitively but differ in
// casing, the body value is replaced with the stateBody value.
func normalizeBodyCasing(body, stateBody []byte) []byte {
	var bodyMap, stateMap interface{}
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return body
	}
	if err := json.Unmarshal(stateBody, &stateMap); err != nil {
		return body
	}
	normalizeCasingRecursive(bodyMap, stateMap)
	result, err := json.Marshal(bodyMap)
	if err != nil {
		return body
	}
	return result
}

// normalizeCasingRecursive walks body and state in parallel and for any
// string leaf where the values are case-insensitively equal, overwrites
// the body value with the state value (preserving config/state casing).
func normalizeCasingRecursive(body, state interface{}) {
	switch bm := body.(type) {
	case map[string]interface{}:
		sm, ok := state.(map[string]interface{})
		if !ok {
			return
		}
		for k, sv := range sm {
			bv, exists := bm[k]
			if !exists {
				continue
			}
			normalizeCasingRecursive(bv, sv)
			bStr, bOk := bv.(string)
			sStr, sOk := sv.(string)
			if bOk && sOk && strings.EqualFold(bStr, sStr) && bStr != sStr {
				bm[k] = sStr
			}
		}
	case []interface{}:
		sa, ok := state.([]interface{})
		if !ok {
			return
		}
		for i := range bm {
			if i >= len(sa) {
				break
			}
			normalizeCasingRecursive(bm[i], sa[i])
			bStr, bOk := bm[i].(string)
			sStr, sOk := sa[i].(string)
			if bOk && sOk && strings.EqualFold(bStr, sStr) && bStr != sStr {
				bm[i] = sStr
			}
		}
	}
}
