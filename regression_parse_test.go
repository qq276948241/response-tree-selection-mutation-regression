package ajson

// Parse regression tests: a realistic collection response (nested objects,
// heterogeneous arrays, nulls, keys with dots and quotes, escapes, multibyte
// text, big integers) is checked against independently authored expectations.
// Error inputs (truncation, illegal escapes, trailing garbage) are checked
// separately. Only the Go standard library is used for validation.

import (
	"bytes"
	"encoding/json"
	"testing"
)

type parseExpectationFile struct {
	Values []struct {
		Path   string      `json:"path"`
		Type   string      `json:"type"`
		Value  interface{} `json:"value"`
		Source string      `json:"source"`
	} `json:"values"`
	Sizes []struct {
		Path string `json:"path"`
		Size int    `json:"size"`
	} `json:"sizes"`
	Missing []string `json:"missing"`
	Nulls   []string `json:"nulls"`
}

func loadParseExpectations(t *testing.T) parseExpectationFile {
	t.Helper()
	data := readTestData(t, expectParse)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var expectations parseExpectationFile
	if err := dec.Decode(&expectations); err != nil {
		t.Fatalf("decode %s: %v", expectParse, err)
	}
	return expectations
}

func TestRegressionParse_Values(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	expectations := loadParseExpectations(t)

	for _, check := range expectations.Values {
		nodes, err := root.JSONPath(check.Path)
		if err != nil {
			t.Errorf("JSONPath(%q): %v", check.Path, err)
			continue
		}
		if len(nodes) != 1 {
			t.Errorf("JSONPath(%q): %d hits, expected 1", check.Path, len(nodes))
			continue
		}
		node := nodes[0]
		switch check.Type {
		case "Null":
			if !node.IsNull() {
				t.Errorf("%s: expected Null, got %v", check.Path, node.Type())
			}
		case "String":
			expected, ok := check.Value.(string)
			if !ok {
				t.Fatalf("%s: expectation value is not a string", check.Path)
			}
			got, err := node.GetString()
			if err != nil {
				t.Errorf("%s: GetString: %v", check.Path, err)
			} else if got != expected {
				t.Errorf("%s: got %q, expected %q", check.Path, got, expected)
			}
		case "Bool":
			expected, ok := check.Value.(bool)
			if !ok {
				t.Fatalf("%s: expectation value is not a bool", check.Path)
			}
			got, err := node.GetBool()
			if err != nil {
				t.Errorf("%s: GetBool: %v", check.Path, err)
			} else if got != expected {
				t.Errorf("%s: got %v, expected %v", check.Path, got, expected)
			}
		case "Numeric":
			if check.Source != "" {
				// Big integers must keep their original literal value.
				if got := string(node.Source()); got != check.Source {
					t.Errorf("%s: source %q, expected original literal %q", check.Path, got, check.Source)
				}
				continue
			}
			number, ok := check.Value.(json.Number)
			if !ok {
				t.Fatalf("%s: expectation value is not a number", check.Path)
			}
			expected, err := number.Float64()
			if err != nil {
				t.Fatalf("%s: bad expectation number %q", check.Path, number)
			}
			got, err := node.GetNumeric()
			if err != nil {
				t.Errorf("%s: GetNumeric: %v", check.Path, err)
			} else if got != expected {
				t.Errorf("%s: got %v, expected %v", check.Path, got, expected)
			}
		default:
			t.Fatalf("%s: unknown expectation type %q", check.Path, check.Type)
		}
	}
}

func TestRegressionParse_Sizes(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	expectations := loadParseExpectations(t)

	for _, check := range expectations.Sizes {
		node := queryOne(t, root, check.Path)
		if got := node.Size(); got != check.Size {
			t.Errorf("%s: size %d, expected %d", check.Path, got, check.Size)
		}
	}
}

// TestRegressionParse_MissingVsNull makes sure a missing field and a field
// holding an explicit null are distinguishable, and that lookups of missing
// fields fail instead of yielding null nodes.
func TestRegressionParse_MissingVsNull(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	expectations := loadParseExpectations(t)

	for _, path := range expectations.Missing {
		nodes, err := root.JSONPath(path)
		if err != nil {
			t.Errorf("missing %s: JSONPath error: %v", path, err)
			continue
		}
		if len(nodes) != 0 {
			t.Errorf("missing %s: %d hits, expected empty result", path, len(nodes))
		}
	}
	for _, path := range expectations.Nulls {
		node := queryOne(t, root, path)
		if !node.IsNull() {
			t.Errorf("null %s: expected Null node, got %v", path, node.Type())
		}
	}

	// Node API level: HasKey distinguishes missing from explicit null.
	rec001 := queryOne(t, root, `$.records[?(@.id == 'REC-001')]`)
	rec004 := queryOne(t, root, `$.records[?(@.id == 'REC-004')]`)
	if !rec001.HasKey("discount") || !rec001.MustKey("discount").IsNull() {
		t.Errorf("REC-001.discount: expected explicit null")
	}
	if rec004.HasKey("discount") {
		t.Errorf("REC-004.discount: expected missing key")
	}
	if _, err := rec004.GetKey("discount"); err == nil {
		t.Errorf("REC-004.GetKey(discount): expected error for missing key")
	}
	// Empty string is neither missing nor null.
	if !rec004.HasKey("note") {
		t.Errorf("REC-004.note: expected present empty string")
	}
	if note := rec004.MustKey("note"); note.IsNull() {
		t.Errorf("REC-004.note: empty string must not become null")
	} else if got := note.MustString(); got != "" {
		t.Errorf("REC-004.note: got %q, expected empty string", got)
	}
}

// TestRegressionParse_BigIntegerPreserved checks that an unmodified integer
// above 2^53 keeps its exact literal through parse and re-marshal.
func TestRegressionParse_BigIntegerPreserved(t *testing.T) {
	root, data := parseSample(t, sampleCollection)

	shard := queryOne(t, root, `$.meta.shard_id`)
	if got := string(shard.Source()); got != "9007199254740993" {
		t.Errorf("shard_id source %q, expected exact literal 9007199254740993", got)
	}

	// Re-marshaling the untouched tree must reproduce the input byte for byte.
	out, err := Marshal(root)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(out, bytes.TrimSpace(data)) {
		t.Errorf("marshal of unmodified tree differs from input:\n got: %s\nwant: %s", out, bytes.TrimSpace(data))
	}
}

// TestRegressionParse_Errors checks truncation, illegal escapes and trailing
// garbage each fail separately, returning no root node.
func TestRegressionParse_Errors(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		wantType ErrorType
	}{
		{"truncated", "testdata/samples/invalid_truncated.json", UnexpectedEOF},
		{"illegal_escape", "testdata/samples/invalid_escape.json", WrongSymbol},
		{"trailing_garbage", "testdata/samples/invalid_trailing_garbage.json", WrongSymbol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := readTestData(t, tc.path)
			root, err := Unmarshal(data)
			if err == nil {
				t.Fatalf("expected error for %s, got root %s", tc.path, root)
			}
			if root != nil {
				t.Errorf("expected nil root on error, got %s", root)
			}
			aerr, ok := err.(Error)
			if !ok {
				t.Fatalf("expected ajson.Error, got %T: %v", err, err)
			}
			if aerr.Type != tc.wantType {
				t.Errorf("error type %v, expected %v (%v)", aerr.Type, tc.wantType, aerr)
			}
		})
	}
}

// TestRegressionParse_ErrorsInline covers the same failure classes with
// minimal inline inputs, including truncated unicode escapes.
func TestRegressionParse_ErrorsInline(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"truncated_object", `{"a":1`},
		{"truncated_array", `[1,2,`},
		{"truncated_string", `{"a":"bc`},
		{"truncated_literal", `nul`},
		{"empty_input", ``},
		{"illegal_escape", `{"a":"\x"}`},
		{"illegal_unicode_escape", `{"a":"\uZZZZ"}`},
		{"truncated_unicode_escape", `{"a":"\u12"}`},
		{"trailing_garbage_object", `{"a":1} {"b":2}`},
		{"trailing_garbage_array", `[1,2] [3]`},
		{"trailing_garbage_scalar", `1 2`},
		{"trailing_comma", `{"a":1,}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := Unmarshal([]byte(tc.input))
			if err == nil {
				t.Errorf("expected error for %q, got root %s", tc.input, root)
			}
			if root != nil {
				t.Errorf("expected nil root on error, got %s", root)
			}
		})
	}
}
