package ajson

// JSONPath regression tests: recursive descent, conditional filters, array
// slices and special-key access are combined over the parsed collection
// response; hits are verified by business record IDs. The same queries are
// run on a freshly parsed tree (cold) and after all node values have been
// read (pre-read), and must agree.

import (
	"encoding/json"
	"testing"
)

type jsonPathExpectationFile struct {
	Queries []struct {
		Name   string   `json:"name"`
		Query  string   `json:"query"`
		Expect []string `json:"expect"`
	} `json:"queries"`
}

func loadJSONPathExpectations(t *testing.T) jsonPathExpectationFile {
	t.Helper()
	var expectations jsonPathExpectationFile
	if err := json.Unmarshal(readTestData(t, expectJSONPath), &expectations); err != nil {
		t.Fatalf("decode %s: %v", expectJSONPath, err)
	}
	return expectations
}

// TestRegressionJSONPath_Hits runs every expectation query on a freshly
// parsed tree and compares the ordered hit values (record IDs for record
// queries) with the independently authored expectations.
func TestRegressionJSONPath_Hits(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	expectations := loadJSONPathExpectations(t)

	for _, check := range expectations.Queries {
		t.Run(check.Name, func(t *testing.T) {
			got := queryStrings(t, root, check.Query)
			if !equalStrings(got, check.Expect) {
				t.Errorf("query %s:\n got: %q\nwant: %q", check.Query, got, check.Expect)
			}
		})
	}
}

// TestRegressionJSONPath_EmptyResultsAreEmpty makes sure queries with no
// match produce an empty slice — not a slice padded with null nodes.
func TestRegressionJSONPath_EmptyResultsAreEmpty(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	for _, query := range []string{
		`$.missing.path`,
		`$.records[?(@.nonexistent > 1)].id`,
		`$.records[?(@.kind == 'shipment')].id`,
		`$.records[10:20].id`,
	} {
		nodes, err := root.JSONPath(query)
		if err != nil {
			t.Errorf("JSONPath(%q): %v", query, err)
			continue
		}
		if len(nodes) != 0 {
			t.Errorf("JSONPath(%q): %d hits, expected empty result", query, len(nodes))
			for i, node := range nodes {
				if node == nil {
					t.Errorf("JSONPath(%q): hit %d is nil", query, i)
				} else if node.IsNull() {
					t.Errorf("JSONPath(%q): hit %d is a padding null node", query, i)
				}
			}
		}
	}
}

// TestRegressionJSONPath_ColdVsPreRead runs every expectation query twice:
// once on a freshly parsed tree (no value calculated yet) and once after the
// whole tree has been unpacked (all node values read). Results must agree,
// including hit order.
func TestRegressionJSONPath_ColdVsPreRead(t *testing.T) {
	expectations := loadJSONPathExpectations(t)

	cold, _ := parseSample(t, sampleCollection)
	coldHits := make(map[string][]string)
	for _, check := range expectations.Queries {
		coldHits[check.Name] = queryStrings(t, cold, check.Query)
	}

	warm, _ := parseSample(t, sampleCollection)
	if _, err := warm.Unpack(); err != nil {
		t.Fatalf("pre-read Unpack: %v", err)
	}
	for _, check := range expectations.Queries {
		warmHits := queryStrings(t, warm, check.Query)
		if !equalStrings(coldHits[check.Name], warmHits) {
			t.Errorf("query %s (%q) differs between cold and pre-read runs:\ncold: %q\nwarm: %q",
				check.Name, check.Query, coldHits[check.Name], warmHits)
		}
	}
}

// TestRegressionJSONPath_FilterDistinguishesMissingFromNull documents that
// an equality filter against null matches explicit nulls only, not records
// where the field is absent.
func TestRegressionJSONPath_FilterDistinguishesMissingFromNull(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)

	// REC-004 has no "discount" key at all; the others carry explicit nulls
	// or a number. The null-equality filter must hit explicit nulls only.
	got := queryStrings(t, root, `$.records[?(@.discount == null)].id`)
	want := []string{"REC-001", "REC-003", "REC-005"}
	if !equalStrings(got, want) {
		t.Errorf("discount == null: got %q, want %q (missing field must not match)", got, want)
	}

	// Empty string matches only the record whose note is present and empty.
	got = queryStrings(t, root, `$.records[?(@.note == '')].id`)
	want = []string{"REC-004"}
	if !equalStrings(got, want) {
		t.Errorf("note == '': got %q, want %q", got, want)
	}
}

// TestRegressionJSONPath_ArrayHitOrder checks that slice and filter hits
// keep the document index order of the array.
func TestRegressionJSONPath_ArrayHitOrder(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)

	nodes, err := root.JSONPath(`$.records[::2]`)
	if err != nil {
		t.Fatalf("JSONPath: %v", err)
	}
	wantIndex := []int{0, 2, 4}
	if len(nodes) != len(wantIndex) {
		t.Fatalf("got %d hits, want %d", len(nodes), len(wantIndex))
	}
	for i, node := range nodes {
		if got := node.Index(); got != wantIndex[i] {
			t.Errorf("hit %d: index %d, want %d", i, got, wantIndex[i])
		}
	}
}
