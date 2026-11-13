package ajson

// Defect regression tests.
//
// These tests document a confirmed defect of the current implementation:
// the cached container value (Node.value, populated by Node.getValue on the
// first GetArray/GetObject/Value call) is NOT invalidated when the
// container's membership changes. Node.update resets the cache, but
// Node.remove, Node.appendNode and Node.mark (node_mutations.go) do not.
//
// Impact: after reading a container's value and then adding/removing an
// element, GetArray/GetObject/Value return the stale membership, while
// Size(), GetIndex/GetKey, Unpack, JSONPath and Marshal all reflect the new
// state. Any read-modify-read workflow through Value()/GetArray/GetObject
// observes inconsistent data.
//
// The assertions below express the EXPECTED behavior and therefore FAIL
// against the current implementation. They are kept failing on purpose as
// minimal reproductions; the production code is intentionally left
// unchanged. See REGRESSION.md for the full diagnosis.

import "testing"

// Minimal repro: read an array's value, delete the middle element, read
// again — the cached slice still contains the removed element.
func TestDefect_StaleArrayValueCacheAfterDeleteIndex(t *testing.T) {
	root := Must(Unmarshal([]byte(`{"items":[1,2,3]}`)))
	items := root.MustKey("items")

	before, err := items.GetArray() // populates the cached value
	if err != nil {
		t.Fatalf("GetArray before: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("before: %d elements, want 3", len(before))
	}

	if err := items.DeleteIndex(1); err != nil {
		t.Fatalf("DeleteIndex: %v", err)
	}
	if got := items.Size(); got != 2 {
		t.Fatalf("Size after delete: %d, want 2", got)
	}

	after, err := items.GetArray()
	if err != nil {
		t.Fatalf("GetArray after: %v", err)
	}
	if len(after) != 2 {
		t.Errorf("stale cache: GetArray after DeleteIndex returned %d elements, want 2 "+
			"(Node.remove does not invalidate Node.value; see node_mutations.go:remove)", len(after))
	} else {
		if got := after[0].MustNumeric(); got != 1 {
			t.Errorf("after[0]: got %v, want 1", got)
		}
		if got := after[1].MustNumeric(); got != 3 {
			t.Errorf("after[1]: got %v, want 3", got)
		}
	}
}

// Minimal repro: appending to a previously read array is invisible through
// GetArray afterwards.
func TestDefect_StaleArrayValueCacheAfterAppendArray(t *testing.T) {
	root := Must(Unmarshal([]byte(`{"items":[1,2]}`)))
	items := root.MustKey("items")

	if _, err := items.GetArray(); err != nil { // populates the cached value
		t.Fatalf("GetArray before: %v", err)
	}
	if err := items.AppendArray(NumericNode("", 3)); err != nil {
		t.Fatalf("AppendArray: %v", err)
	}
	if got := items.Size(); got != 3 {
		t.Fatalf("Size after append: %d, want 3", got)
	}

	after, err := items.GetArray()
	if err != nil {
		t.Fatalf("GetArray after: %v", err)
	}
	if len(after) != 3 {
		t.Errorf("stale cache: GetArray after AppendArray returned %d elements, want 3 "+
			"(Node.appendNode does not invalidate Node.value; see node_mutations.go:appendNode)", len(after))
	}
}

// Minimal repro: deleting a key from a previously read object is invisible
// through GetObject afterwards.
func TestDefect_StaleObjectValueCacheAfterDeleteKey(t *testing.T) {
	root := Must(Unmarshal([]byte(`{"meta":{"a":1,"b":2}}`)))
	meta := root.MustKey("meta")

	before, err := meta.GetObject() // populates the cached value
	if err != nil {
		t.Fatalf("GetObject before: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("before: %d keys, want 2", len(before))
	}

	if err := meta.DeleteKey("a"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if got := meta.Size(); got != 1 {
		t.Fatalf("Size after delete: %d, want 1", got)
	}

	after, err := meta.GetObject()
	if err != nil {
		t.Fatalf("GetObject after: %v", err)
	}
	if len(after) != 1 {
		t.Errorf("stale cache: GetObject after DeleteKey returned %d keys, want 1 "+
			"(Node.remove does not invalidate Node.value; see node_mutations.go:remove)", len(after))
	}
	if _, ok := after["a"]; ok {
		t.Errorf("stale cache: deleted key 'a' still present in GetObject result")
	}
}

// Minimal repro: adding a key to a previously read object is invisible
// through GetObject afterwards.
func TestDefect_StaleObjectValueCacheAfterAppendObject(t *testing.T) {
	root := Must(Unmarshal([]byte(`{"meta":{"a":1}}`)))
	meta := root.MustKey("meta")

	if _, err := meta.GetObject(); err != nil { // populates the cached value
		t.Fatalf("GetObject before: %v", err)
	}
	if err := meta.AppendObject("b", NumericNode("b", 2)); err != nil {
		t.Fatalf("AppendObject: %v", err)
	}
	if got := meta.Size(); got != 2 {
		t.Fatalf("Size after append: %d, want 2", got)
	}

	after, err := meta.GetObject()
	if err != nil {
		t.Fatalf("GetObject after: %v", err)
	}
	if len(after) != 2 {
		t.Errorf("stale cache: GetObject after AppendObject returned %d keys, want 2 "+
			"(Node.appendNode does not invalidate Node.value; see node_mutations.go:appendNode)", len(after))
	}
	if _, ok := after["b"]; !ok {
		t.Errorf("stale cache: appended key 'b' missing from GetObject result")
	}
}
