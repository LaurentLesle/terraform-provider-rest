package provider

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/LaurentLesle/terraform-provider-rest/internal/attrpath"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ModifyBody modifies the body based on the base body, only keeps attributes that exist on both sides.
// If compensateBaseAttrs is set, then any attribute path element only found in the base body will
// be added up to the result body.
func ModifyBody(base, body string, compensateBaseAttrs []string) (string, error) {
	var baseJSON any
	if err := json.Unmarshal([]byte(base), &baseJSON); err != nil {
		return "", fmt.Errorf("unmarshal the base %q: %v", base, err)
	}

	var bodyJSON any
	if err := json.Unmarshal([]byte(body), &bodyJSON); err != nil {
		return "", fmt.Errorf("unmarshal the body %q: %v", body, err)
	}

	b, err := json.Marshal(getUpdatedJSON(baseJSON, bodyJSON))
	if err != nil {
		return "", err
	}
	result := string(b)

	for _, path := range compensateBaseAttrs {
		if gjson.Get(base, path).Exists() && !gjson.Get(body, path).Exists() {
			var err error
			result, err = sjson.Set(result, path, gjson.Get(base, path).Value())
			if err != nil {
				return "", err
			}
		}
	}

	// Remarshal to keep order.
	var m any
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		return "", err
	}
	b, err = json.Marshal(m)
	return string(b), err
}

func getUpdatedJSON(oldJSON, newJSON any) any {
	switch oldJSON := oldJSON.(type) {
	case map[string]any:
		if newJSON, ok := newJSON.(map[string]any); ok {
			out := map[string]any{}
			for k, ov := range oldJSON {
				if nv, ok := newJSON[k]; ok {
					out[k] = getUpdatedJSON(ov, nv)
				}
			}
			return out
		}
	case []any:
		if newJSON, ok := newJSON.([]any); ok {
			if len(newJSON) != len(oldJSON) {
				return newJSON
			}
			out := make([]any, len(newJSON))
			for i := range newJSON {
				out[i] = getUpdatedJSON(oldJSON[i], newJSON[i])
			}
			return out
		}
	}
	return newJSON
}

// ModifyBodyForImport is similar as ModifyBody, but is based on the body from import spec, rather than from state.
func ModifyBodyForImport(base, body string) (string, error) {
	// This happens when importing resource without specifying the "body", where there is no state for "body".
	if base == "" {
		return body, nil
	}
	var baseJSON any
	if err := json.Unmarshal([]byte(base), &baseJSON); err != nil {
		return "", fmt.Errorf("unmarshal the base %q: %v", base, err)
	}
	var bodyJSON any
	if err := json.Unmarshal([]byte(body), &bodyJSON); err != nil {
		return "", fmt.Errorf("unmarshal the body %q: %v", body, err)
	}
	updatedBody, err := getUpdatedJSONForImport(baseJSON, bodyJSON)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(updatedBody)
	return string(b), err
}

func getUpdatedJSONForImport(oldJSON, newJSON any) (any, error) {
	switch oldJSON := oldJSON.(type) {
	case map[string]any:
		if newJSON, ok := newJSON.(map[string]any); ok {
			out := map[string]any{}
			for k, ov := range oldJSON {
				if nv, ok := newJSON[k]; ok {
					var err error
					out[k], err = getUpdatedJSONForImport(ov, nv)
					if err != nil {
						return nil, fmt.Errorf("failed to update json for key %q: %v", k, err)
					}
				}
			}
			return out, nil
		}
	case []any:
		if newJSON, ok := newJSON.([]any); ok {
			switch len(oldJSON) {
			case 0:
				// The same as setting to null, just return the newJSON.
				return newJSON, nil
			case 1:
				out := make([]any, len(newJSON))
				for i := range newJSON {
					var err error
					out[i], err = getUpdatedJSONForImport(oldJSON[0], newJSON[i])
					if err != nil {
						return nil, fmt.Errorf("failed to update json for the %dth element: %v", i, err)
					}
				}
				return out, nil
			default:
				return newJSON, fmt.Errorf("the length of array should be 1")
			}
		}
	}
	return newJSON, nil
}

type ObjectOrArray any

// Given a JSON object, only keep the attributes specified and remove the others.
func FilterAttrsInJSON(doc string, attrs []string) (string, error) {
	if len(attrs) == 0 {
		return doc, nil
	}

	var paths []attrpath.AttrPath
	for _, attr := range attrs {
		path, err := attrpath.Path(attr)
		if err != nil {
			return "", fmt.Errorf("parsing %q: %v", attr, err)
		}
		paths = append(paths, path)
	}

	var jsonDoc any
	if err := json.Unmarshal([]byte(doc), &jsonDoc); err != nil {
		return "", err
	}

	var odoc any
	for _, path := range paths {
		doc, err := filterAttrInJSON(jsonDoc, attrpath.AttrPath{}, path)
		if err != nil {
			return "", err
		}
		if odoc == nil {
			odoc = doc
			continue
		}
		switch d := odoc.(type) {
		case map[string]any:
			dd, ok := doc.(map[string]any)
			if !ok {
				return "", fmt.Errorf("expect value %v to be a map, but got %T", doc, doc)
			}
			odoc, err = mergeObject("", d, dd)
			if err != nil {
				return "", err
			}
		case []any:
			dd, ok := doc.([]any)
			if !ok {
				return "", fmt.Errorf("expect value %v to be an array, but got %T", doc, doc)
			}
			odoc, err = mergeArray("", d, dd)
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unsupported types for JSON attr filtering: %T", odoc)
		}
	}
	b, err := json.Marshal(odoc)
	if err != nil {
		return "", fmt.Errorf("marshalling the filtered document: %v", err)
	}
	return string(b), nil
}

func filterAttrInJSON(doc any, prefix, path attrpath.AttrPath) (any, error) {
	if len(path) == 0 {
		var odoc any
		b, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &odoc); err != nil {
			return nil, err
		}
		return odoc, nil
	}

	step := path[0]
	prefix = append(prefix, step)
	remain := path[1:]
	switch doc := doc.(type) {
	case []any:
		switch step := step.(type) {
		case attrpath.AttrStepValue:
			// This must be an splat
			return nil, fmt.Errorf("%s: expect a splat step, got a value step (%s)", prefix, step)
		case attrpath.AttrStepSplat:
			odoc := []any{}
			for i := range doc {
				indoc, err := filterAttrInJSON(doc[i], prefix, remain)
				if err != nil {
					return nil, err
				}
				odoc = append(odoc, indoc)
			}
			return odoc, nil
		default:
			return nil, fmt.Errorf("%s: unknown step type %T", prefix, step)
		}
	case map[string]any:
		switch step := step.(type) {
		case attrpath.AttrStepValue:
			odoc := map[string]any{}
			k := string(step)
			v, ok := doc[k]
			if !ok {
				return odoc, nil
			}
			indoc, err := filterAttrInJSON(v, prefix, remain)
			if err != nil {
				return nil, err
			}
			odoc[k] = indoc
			return odoc, nil
		case attrpath.AttrStepSplat:
			return nil, fmt.Errorf("%s: expect a value step, got a splat step", prefix)
		default:
			return nil, fmt.Errorf("%s: unknown step type %T", prefix, step)
		}
	default:
		return nil, fmt.Errorf("invalid document type %T", doc)
	}
}

func mergeArray(addr string, arr1, arr2 []any) ([]any, error) {
	if len(arr1) != len(arr2) {
		return nil, fmt.Errorf("%s: length not the same %d != %d", addr, len(arr1), len(arr2))
	}
	arr := []any{}
	for i := range arr1 {
		nextaddr := addr + "." + strconv.Itoa(i)
		e1, e2 := arr1[i], arr2[i]
		switch e1 := e1.(type) {
		case map[string]any:
			e2, ok := e2.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: expect value %v to be a map, but got %T", nextaddr, e2, e2)
			}
			e, err := mergeObject(nextaddr, e1, e2)
			if err != nil {
				return nil, fmt.Errorf("merging %s: %v", nextaddr, err)
			}
			arr = append(arr, e)
		case []any:
			e2, ok := e2.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: expect value %v to be an array, but got %T", nextaddr, e2, e2)
			}
			e, err := mergeArray(nextaddr, e1, e2)
			if err != nil {
				return nil, fmt.Errorf("merging %s: %v", nextaddr, err)
			}
			arr = append(arr, e)
		default:
			if e1 != e2 {
				return nil, fmt.Errorf("%s: two values are not the same: %v != %v", nextaddr, e1, e2)
			}
			arr = append(arr, e1)
		}
	}
	return arr, nil
}

func mergeObject(addr string, obj1, obj2 map[string]any) (map[string]any, error) {
	obj := map[string]any{}
	intersecKey := map[string]bool{}
	for k1, v1 := range obj1 {
		if _, ok := obj2[k1]; !ok {
			obj[k1] = v1
			continue
		}
		intersecKey[k1] = true
	}
	for k2, v2 := range obj2 {
		if _, ok := obj1[k2]; !ok {
			obj[k2] = v2
		}
	}
	var err error
	for k := range intersecKey {
		v1, v2 := obj1[k], obj2[k]
		nextaddr := addr + "." + k
		switch v1 := v1.(type) {
		case map[string]any:
			v2, ok := v2.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: expect value %v to be a map, but got %T", nextaddr, v2, v2)
			}
			obj[k], err = mergeObject(nextaddr, v1, v2)
			if err != nil {
				return nil, fmt.Errorf("merging %s: %v", nextaddr, err)
			}
		case []any:
			v2, ok := v2.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: expect value %v to be an array, but got %T", nextaddr, v2, v2)
			}
			obj[k], err = mergeArray(nextaddr, v1, v2)
			if err != nil {
				return nil, fmt.Errorf("merging %s: %v", nextaddr, err)
			}
		default:
			if v1 != v2 {
				return nil, fmt.Errorf("%s: two values are not the same: %v != %v", nextaddr, v1, v2)
			}
			obj[k] = v1
		}
	}
	return obj, nil
}
