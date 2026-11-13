package ajson

// Shared helpers for the response-processing regression suite.
//
// Expectations live under testdata/expected and were authored by hand from
// the business scenario (they are NOT generated from this library's output).
// encoding/json with UseNumber is used to decode expectations so that number
// literals (including integers above 2^53) stay exact.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
)

const (
	sampleCollection = "testdata/samples/collection_response.json"

	expectParse    = "testdata/expected/parse_expectations.json"
	expectJSONPath = "testdata/expected/jsonpath_expectations.json"

	expectStage1 = "testdata/expected/mutation_stage1_replace.json"
	expectStage2 = "testdata/expected/mutation_stage2_delete.json"
	expectStage3 = "testdata/expected/mutation_stage3_copy.json"
	expectStage4 = "testdata/expected/mutation_stage4_move.json"
)

func readTestData(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func parseSample(t *testing.T, path string) (*Node, []byte) {
	t.Helper()
	data := readTestData(t, path)
	root, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal(%s): %v", path, err)
	}
	if root == nil {
		t.Fatalf("Unmarshal(%s): nil root without error", path)
	}
	return root, data
}

// decodeExpectation decodes a JSON expectation file keeping number literals
// exact (json.Number), independent of this library's parsing.
func decodeExpectation(t *testing.T, path string) interface{} {
	t.Helper()
	data := readTestData(t, path)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		t.Fatalf("decode expectation %s: %v", path, err)
	}
	return value
}

// assertNodeStructure compares the given node tree against an independently
// authored expectation (decoded with encoding/json, UseNumber). Array order
// is enforced; object key order is not.
func assertNodeStructure(t *testing.T, path string, node *Node, expected interface{}) {
	t.Helper()
	if node == nil {
		t.Errorf("%s: nil node, expected %v", path, expected)
		return
	}
	switch exp := expected.(type) {
	case nil:
		if !node.IsNull() {
			t.Errorf("%s: expected null, got type %v", path, node.Type())
		}
	case json.Number:
		if !node.IsNumeric() {
			t.Errorf("%s: expected numeric %s, got type %v", path, exp, node.Type())
			return
		}
		if src := node.Source(); src != nil {
			// Unmodified node: the original literal must be preserved exactly.
			if string(src) != exp.String() {
				t.Errorf("%s: numeric literal %q, expected %q", path, src, exp)
			}
			return
		}
		f, err := node.GetNumeric()
		if err != nil {
			t.Errorf("%s: GetNumeric: %v", path, err)
			return
		}
		if got := strconv.FormatFloat(f, 'g', -1, 64); got != exp.String() {
			t.Errorf("%s: numeric %s, expected %s", path, got, exp)
		}
	case string:
		if !node.IsString() {
			t.Errorf("%s: expected string %q, got type %v", path, exp, node.Type())
			return
		}
		got, err := node.GetString()
		if err != nil {
			t.Errorf("%s: GetString: %v", path, err)
			return
		}
		if got != exp {
			t.Errorf("%s: string %q, expected %q", path, got, exp)
		}
	case bool:
		if !node.IsBool() {
			t.Errorf("%s: expected bool %v, got type %v", path, exp, node.Type())
			return
		}
		got, err := node.GetBool()
		if err != nil {
			t.Errorf("%s: GetBool: %v", path, err)
			return
		}
		if got != exp {
			t.Errorf("%s: bool %v, expected %v", path, got, exp)
		}
	case []interface{}:
		if !node.IsArray() {
			t.Errorf("%s: expected array of %d elements, got type %v", path, len(exp), node.Type())
			return
		}
		if node.Size() != len(exp) {
			t.Errorf("%s: array size %d, expected %d", path, node.Size(), len(exp))
			return
		}
		for i, item := range exp {
			child, err := node.GetIndex(i)
			if err != nil {
				t.Errorf("%s[%d]: %v", path, i, err)
				continue
			}
			assertNodeStructure(t, fmt.Sprintf("%s[%d]", path, i), child, item)
		}
	case map[string]interface{}:
		if !node.IsObject() {
			t.Errorf("%s: expected object with %d keys, got type %v", path, len(exp), node.Type())
			return
		}
		if node.Size() != len(exp) {
			t.Errorf("%s: object has keys %v, expected %d keys", path, node.Keys(), len(exp))
		}
		for key, item := range exp {
			child, err := node.GetKey(key)
			if err != nil {
				t.Errorf("%s: missing key %q", path, key)
				continue
			}
			assertNodeStructure(t, path+"["+strconv.Quote(key)+"]", child, item)
		}
	default:
		t.Fatalf("%s: unsupported expectation type %T", path, expected)
	}
}

// queryStrings runs a JSONPath query and returns the string value of every
// hit, in hit order. It fails if any hit is nil or not a String node, which
// also guards against empty results being padded with null nodes.
func queryStrings(t *testing.T, root *Node, query string) []string {
	t.Helper()
	nodes, err := root.JSONPath(query)
	if err != nil {
		t.Fatalf("JSONPath(%q): %v", query, err)
	}
	result := make([]string, 0, len(nodes))
	for i, node := range nodes {
		if node == nil {
			t.Fatalf("JSONPath(%q): hit %d is a nil node", query, i)
		}
		if node.IsNull() {
			t.Fatalf("JSONPath(%q): hit %d is an unexpected null node", query, i)
		}
		value, err := node.GetString()
		if err != nil {
			t.Fatalf("JSONPath(%q): hit %d is not a string: %v", query, i, err)
		}
		result = append(result, value)
	}
	return result
}

// queryOne runs a JSONPath query that must match exactly one node.
func queryOne(t *testing.T, root *Node, query string) *Node {
	t.Helper()
	nodes, err := root.JSONPath(query)
	if err != nil {
		t.Fatalf("JSONPath(%q): %v", query, err)
	}
	if len(nodes) != 1 {
		t.Fatalf("JSONPath(%q): %d hits, expected 1", query, len(nodes))
	}
	if nodes[0] == nil {
		t.Fatalf("JSONPath(%q): hit is a nil node", query)
	}
	return nodes[0]
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Mutation stages -------------------------------------------------------
// Each stage locates its targets through JSONPath (so rewrites land on the
// actually matched objects) and is shared between the mutation regression
// test and the marshal round-trip test.

// stageReplace rewrites scalar values deep in the tree. New numeric values
// stay within the exactly representable float64 range.
func stageReplace(t *testing.T, root *Node) {
	t.Helper()
	rec002 := queryOne(t, root, `$.records[?(@.id == 'REC-002')]`)
	if err := rec002.MustKey("amount").SetNumeric(79.5); err != nil {
		t.Fatalf("stageReplace amount: %v", err)
	}
	page := queryOne(t, root, `$.meta.page`)
	if err := page.SetNumeric(2); err != nil {
		t.Fatalf("stageReplace page: %v", err)
	}
	note := queryOne(t, root, `$.records[?(@.id == 'REC-001')].note`)
	if err := note.SetString("已签收"); err != nil {
		t.Fatalf("stageReplace note: %v", err)
	}
}

// stageDelete removes the middle record (REC-002) and the tags of REC-004.
func stageDelete(t *testing.T, root *Node) {
	t.Helper()
	records := root.MustKey("records")
	if err := records.DeleteIndex(1); err != nil {
		t.Fatalf("stageDelete DeleteIndex: %v", err)
	}
	rec004 := queryOne(t, root, `$.records[?(@.id == 'REC-004')]`)
	if err := rec004.DeleteKey("tags"); err != nil {
		t.Fatalf("stageDelete DeleteKey: %v", err)
	}
}

// stageCopy clones REC-001, modifies the clone and appends it to records.
func stageCopy(t *testing.T, root *Node) (clone *Node) {
	t.Helper()
	orig := queryOne(t, root, `$.records[?(@.id == 'REC-001')]`)
	clone = orig.Clone()
	if err := clone.MustKey("id").SetString("REC-001-COPY"); err != nil {
		t.Fatalf("stageCopy id: %v", err)
	}
	if err := clone.MustKey("customer").MustKey("name").SetString("Ada Clone"); err != nil {
		t.Fatalf("stageCopy name: %v", err)
	}
	if err := root.MustKey("records").AppendArray(clone); err != nil {
		t.Fatalf("stageCopy AppendArray: %v", err)
	}
	return clone
}

// stageMove moves existing nodes across containers: REC-003's customer
// object and the last element of "mixed" are moved into a new "archive"
// object on the root. Per Node.AppendObject semantics the nodes are
// detached from their previous parents (move, not copy).
func stageMove(t *testing.T, root *Node) {
	t.Helper()
	archive := ObjectNode("archive", nil)
	if err := root.AppendObject("archive", archive); err != nil {
		t.Fatalf("stageMove AppendObject(archive): %v", err)
	}
	customer := queryOne(t, root, `$.records[?(@.id == 'REC-003')].customer`)
	if err := archive.AppendObject("customer", customer); err != nil {
		t.Fatalf("stageMove AppendObject(customer): %v", err)
	}
	pair, err := root.MustKey("mixed").GetIndex(5)
	if err != nil {
		t.Fatalf("stageMove GetIndex(5): %v", err)
	}
	if err := archive.AppendObject("pair", pair); err != nil {
		t.Fatalf("stageMove AppendObject(pair): %v", err)
	}
}

// applyAllStages runs the full replace -> delete -> copy -> move pipeline.
func applyAllStages(t *testing.T, root *Node) {
	t.Helper()
	stageReplace(t, root)
	stageDelete(t, root)
	stageCopy(t, root)
	stageMove(t, root)
}
