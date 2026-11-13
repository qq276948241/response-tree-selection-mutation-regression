package ajson

// Marshal round-trip regression tests: the mutated response is re-encoded,
// parsed again and re-filtered. The full structure and the hit records are
// compared item by item; unchanged fields must survive and changed values
// must come from the new state, not from a stale cache.

import (
	"testing"
)

// TestRegressionMarshal_RoundTrip marshals the fully mutated tree, parses
// the output again and compares the complete structure and the JSONPath
// hits against the expectations. A second round trip must be identical.
func TestRegressionMarshal_RoundTrip(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	applyAllStages(t, root)

	expected := decodeExpectation(t, expectStage4)
	expectedIDs := []string{"REC-001", "REC-003", "REC-004", "REC-005", "REC-001-COPY"}
	expectedOrders := []string{"REC-001", "REC-004", "REC-001-COPY"}

	current := root
	for round := 1; round <= 2; round++ {
		out, err := Marshal(current)
		if err != nil {
			t.Fatalf("round %d: Marshal: %v", round, err)
		}
		reparsed, err := Unmarshal(out)
		if err != nil {
			t.Fatalf("round %d: re-parse of marshaled output: %v\noutput: %s", round, err, out)
		}

		// Full structure, item by item (object key order is not enforced).
		assertNodeStructure(t, "$", reparsed, expected)

		// Hit records by business ID.
		if got := queryStrings(t, reparsed, `$..id`); !equalStrings(got, expectedIDs) {
			t.Errorf("round %d: $..id got %q, want %q", round, got, expectedIDs)
		}
		if got := queryStrings(t, reparsed, `$.records[?(@.kind == 'order')].id`); !equalStrings(got, expectedOrders) {
			t.Errorf("round %d: order ids got %q, want %q", round, got, expectedOrders)
		}
		if got := queryStrings(t, reparsed, `$.archive.customer.name`); !equalStrings(got, []string{"Círó"}) {
			t.Errorf("round %d: archive.customer.name got %q", round, got)
		}

		// Unchanged fields must not be lost; the big integer keeps its
		// exact literal even though ancestors were rewritten.
		shard := queryOne(t, reparsed, `$.meta.shard_id`)
		if got := string(shard.Source()); got != "9007199254740993" {
			t.Errorf("round %d: shard_id literal %q, want 9007199254740993", round, got)
		}
		if got := queryOne(t, reparsed, `$.meta.trace`).MustString(); got != "αβγ-日本語-🚀" {
			t.Errorf("round %d: meta.trace got %q", round, got)
		}
		if got := queryOne(t, reparsed, `$.meta.greeting`).MustString(); got != "line1\nline2\t\"quoted\" \\backslash" {
			t.Errorf("round %d: meta.greeting got %q", round, got)
		}

		// Changed values must be present (no stale cache flowing back).
		if got := queryOne(t, reparsed, `$.records[?(@.id == 'REC-001')].note`).MustString(); got != "已签收" {
			t.Errorf("round %d: REC-001.note got %q, want 已签收", round, got)
		}
		if got := queryOne(t, reparsed, `$.meta.page`).MustNumeric(); got != 2 {
			t.Errorf("round %d: meta.page got %v, want 2", round, got)
		}
		// Deleted and moved content must stay gone.
		if got := queryStrings(t, reparsed, `$.records[?(@.id == 'REC-002')].id`); len(got) != 0 {
			t.Errorf("round %d: deleted REC-002 still matched: %q", round, got)
		}
		if rec003 := queryOne(t, reparsed, `$.records[?(@.id == 'REC-003')]`); rec003.HasKey("customer") {
			t.Errorf("round %d: REC-003.customer present after move", round)
		}

		current = reparsed
	}
}

// TestRegressionMarshal_PartialSubtree checks that a dirty subtree can be
// marshaled on its own and re-parsed into the expected shape.
func TestRegressionMarshal_PartialSubtree(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)
	applyAllStages(t, root)

	out, err := Marshal(root.MustKey("archive"))
	if err != nil {
		t.Fatalf("Marshal(archive): %v", err)
	}
	archive, err := Unmarshal(out)
	if err != nil {
		t.Fatalf("re-parse archive: %v (%s)", err, out)
	}
	expected := decodeExpectation(t, expectStage4).(map[string]interface{})["archive"]
	assertNodeStructure(t, "$.archive", archive, expected)
}
