package ajson

// Node mutation regression tests: the same response is processed through
// replace, delete, copy and cross-container move stages, repeated several
// times on fresh parses. The tree (and subtrees) are fully read before any
// change; after each deep change, ancestor-level queries must reflect it.

import (
	"fmt"
	"testing"
)

// TestRegressionMutation_Stages runs the full pipeline three times to make
// sure repeated processing of the same response is deterministic.
func TestRegressionMutation_Stages(t *testing.T) {
	for run := 0; run < 3; run++ {
		t.Run(fmt.Sprintf("run_%d", run), func(t *testing.T) {
			root, _ := parseSample(t, sampleCollection)

			// Pre-read the whole tree and the subtrees that will be changed.
			if _, err := root.Unpack(); err != nil {
				t.Fatalf("pre-read Unpack: %v", err)
			}
			records := root.MustKey("records")
			if _, err := records.GetArray(); err != nil {
				t.Fatalf("pre-read records: %v", err)
			}

			// Baseline ancestor query before any change.
			if got, want := queryStrings(t, root, `$.records[?(@.amount > 78)].id`),
				[]string{"REC-001", "REC-004", "REC-005"}; !equalStrings(got, want) {
				t.Fatalf("baseline amount>78: got %q, want %q", got, want)
			}

			t.Run("replace", func(t *testing.T) { checkStageReplace(t, root) })
			t.Run("delete", func(t *testing.T) { checkStageDelete(t, root, records) })
			t.Run("copy", func(t *testing.T) { checkStageCopy(t, root, records) })
			t.Run("move", func(t *testing.T) { checkStageMove(t, root) })
		})
	}
}

// checkStageReplace changes deep scalar fields and verifies that ancestor
// queries and the full structure reflect the change.
func checkStageReplace(t *testing.T, root *Node) {
	stageReplace(t, root)

	// The rewrite must land on the actually matched object.
	rec002 := queryOne(t, root, `$.records[?(@.id == 'REC-002')]`)
	if got := rec002.MustKey("amount").MustNumeric(); got != 79.5 {
		t.Errorf("REC-002.amount: got %v, want 79.5", got)
	}
	// Ancestor-level query sees the deep change immediately.
	if got, want := queryStrings(t, root, `$.records[?(@.amount > 78)].id`),
		[]string{"REC-001", "REC-002", "REC-004", "REC-005"}; !equalStrings(got, want) {
		t.Errorf("after replace, amount>78: got %q, want %q", got, want)
	}
	if got := queryOne(t, root, `$.meta.page`).MustNumeric(); got != 2 {
		t.Errorf("meta.page: got %v, want 2", got)
	}
	if got := queryOne(t, root, `$.records[?(@.id == 'REC-001')].note`).MustString(); got != "已签收" {
		t.Errorf("REC-001.note: got %q, want 已签收", got)
	}
	assertNodeStructure(t, "$", root, decodeExpectation(t, expectStage1))
}

// checkStageDelete removes the middle record and checks that indices, parent
// links and subsequent filters still map to the right objects.
func checkStageDelete(t *testing.T, root, records *Node) {
	stageDelete(t, root)

	if got := records.Size(); got != 4 {
		t.Fatalf("records size after delete: got %d, want 4", got)
	}
	wantIDs := []string{"REC-001", "REC-003", "REC-004", "REC-005"}
	for i, want := range wantIDs {
		child, err := records.GetIndex(i)
		if err != nil {
			t.Fatalf("records[%d]: %v", i, err)
		}
		if got := child.MustKey("id").MustString(); got != want {
			t.Errorf("records[%d].id: got %q, want %q", i, got, want)
		}
		if got := child.Index(); got != i {
			t.Errorf("records[%d].Index(): got %d", i, got)
		}
		if child.Parent() != records {
			t.Errorf("records[%d].Parent(): not the records array", i)
		}
	}
	// Subsequent filters must hit the shifted objects.
	if got := queryStrings(t, root, `$.records[1].id`); !equalStrings(got, []string{"REC-003"}) {
		t.Errorf("records[1].id after delete: got %q, want [REC-003]", got)
	}
	if got, want := queryStrings(t, root, `$..id`), wantIDs; !equalStrings(got, want) {
		t.Errorf("$..id after delete: got %q, want %q", got, want)
	}
	// REC-004 lost its tags via DeleteKey.
	rec004 := queryOne(t, root, `$.records[?(@.id == 'REC-004')]`)
	if rec004.HasKey("tags") {
		t.Errorf("REC-004.tags: expected key to be deleted")
	}
	assertNodeStructure(t, "$", root, decodeExpectation(t, expectStage2))
}

// checkStageCopy clones a record, changes the clone and verifies the
// original subtree is untouched.
func checkStageCopy(t *testing.T, root, records *Node) {
	clone := stageCopy(t, root)

	if got := records.Size(); got != 5 {
		t.Fatalf("records size after copy: got %d, want 5", got)
	}
	if clone.Parent() != records || clone.Index() != 4 {
		t.Errorf("clone reference: parent/index not attached to records[4]")
	}
	// The original record is unchanged.
	orig := queryOne(t, root, `$.records[?(@.id == 'REC-001')]`)
	if got := orig.MustKey("customer").MustKey("name").MustString(); got != `Ada "Ace" Lovelace` {
		t.Errorf("original customer name changed by clone edit: %q", got)
	}
	if got := clone.MustKey("customer").MustKey("name").MustString(); got != "Ada Clone" {
		t.Errorf("clone customer name: got %q, want Ada Clone", got)
	}
	if got, want := queryStrings(t, root, `$..id`),
		[]string{"REC-001", "REC-003", "REC-004", "REC-005", "REC-001-COPY"}; !equalStrings(got, want) {
		t.Errorf("$..id after copy: got %q, want %q", got, want)
	}
	assertNodeStructure(t, "$", root, decodeExpectation(t, expectStage3))
}

// checkStageMove moves existing nodes across containers and verifies they
// leave their original position (move semantics, not copy).
func checkStageMove(t *testing.T, root *Node) {
	stageMove(t, root)

	archive := root.MustKey("archive")
	customer := archive.MustKey("customer")
	if got := customer.MustKey("name").MustString(); got != "Círó" {
		t.Errorf("archive.customer.name: got %q, want Círó", got)
	}
	if customer.Parent() != archive {
		t.Errorf("moved customer: parent is not archive")
	}
	// Move detaches from the original location: REC-003 has no customer now.
	rec003 := queryOne(t, root, `$.records[?(@.id == 'REC-003')]`)
	if rec003.HasKey("customer") {
		t.Errorf("REC-003.customer: still present after move (move must not behave like copy)")
	}
	// The array element moved out of "mixed"; remaining elements are reindexed.
	mixed := root.MustKey("mixed")
	if got := mixed.Size(); got != 5 {
		t.Fatalf("mixed size after move: got %d, want 5", got)
	}
	for i := 0; i < mixed.Size(); i++ {
		child := mixed.MustIndex(i)
		if got := child.Index(); got != i {
			t.Errorf("mixed[%d].Index(): got %d", i, got)
		}
		if child.Parent() != mixed {
			t.Errorf("mixed[%d].Parent(): not the mixed array", i)
		}
	}
	pair := archive.MustKey("pair")
	if !pair.IsArray() || pair.Size() != 2 || pair.MustIndex(1).MustNumeric() != 2 {
		t.Errorf("archive.pair: got %s, want [1,2]", pair)
	}
	if got, want := queryStrings(t, root, `$.archive.customer.name`), []string{"Círó"}; !equalStrings(got, want) {
		t.Errorf("archive.customer.name query: got %q, want %q", got, want)
	}
	assertNodeStructure(t, "$", root, decodeExpectation(t, expectStage4))
}

// TestRegressionMutation_AncestorQuerySync isolates the requirement that
// reading the whole tree first does not prevent ancestor queries from
// seeing later deep changes.
func TestRegressionMutation_AncestorQuerySync(t *testing.T) {
	root, _ := parseSample(t, sampleCollection)

	// Read everything, including the value caches of the containers.
	if _, err := root.Unpack(); err != nil {
		t.Fatalf("pre-read: %v", err)
	}
	before := queryStrings(t, root, `$.records[?(@.amount > 100)].id`)
	if want := []string{"REC-001", "REC-004", "REC-005"}; !equalStrings(before, want) {
		t.Fatalf("before: got %q, want %q", before, want)
	}

	// Change a deep field through a JSONPath match.
	queryOne(t, root, `$.records[?(@.id == 'REC-003')].amount`).SetNumeric(150)

	after := queryStrings(t, root, `$.records[?(@.amount > 100)].id`)
	if want := []string{"REC-001", "REC-003", "REC-004", "REC-005"}; !equalStrings(after, want) {
		t.Errorf("after deep change: got %q, want %q (ancestor query must sync)", after, want)
	}
}
