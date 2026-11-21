package apptest_test

import (
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

func TestRecorder_LogsInOrder(t *testing.T) {
	var r apptest.Recorder[string]
	if log := r.Log(); len(log) != 0 {
		t.Errorf("zero Recorder Log() = %q, want empty", log)
	}

	r.Record("a")
	r.Record("b")
	r.Record("a")

	if want := []string{"a", "b", "a"}; !slices.Equal(r.Log(), want) {
		t.Errorf("Log() = %q, want %q", r.Log(), want)
	}
}

func TestRecorder_LogIsACopy(t *testing.T) {
	var r apptest.Recorder[string]
	r.Record("a")

	log := r.Log()
	log[0] = "changed"
	r.Record("b")

	if want := []string{"a", "b"}; !slices.Equal(r.Log(), want) {
		t.Errorf("Log() = %q, want %q", r.Log(), want)
	}
	if log[0] != "changed" || len(log) != 1 {
		t.Errorf("earlier Log() = %q, want it unchanged by later Records", log)
	}
}

func TestRecorder_ConcurrentRecords(t *testing.T) {
	var r apptest.Recorder[string]
	var wg sync.WaitGroup
	const n = 50
	for i := range n {
		wg.Go(func() { r.Record(strconv.Itoa(i)) })
	}
	wg.Wait()

	if got := len(r.Log()); got != n {
		t.Errorf("len(Log()) = %d, want %d", got, n)
	}
}

func TestNames_ReadsEachHandle(t *testing.T) {
	g := graph.New()
	refs := []graph.Ref{g.Value("no color", false), g.Value("lifecycle config", 0)}

	if got, want := apptest.Names(refs), []string{"no color", "lifecycle config"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %q, want %q", got, want)
	}
	if got := apptest.Names(nil); len(got) != 0 {
		t.Errorf("Names(nil) = %q, want empty", got)
	}
}

func TestEnv_ClearsInheritedAndSetsGiven(t *testing.T) {
	t.Setenv("INFER_INHERITED", "inherited")
	t.Setenv("APPTEST_OTHER", "other")

	apptest.Env(t, map[string]string{"INFER_BASE_URL": "http://example.test/v1"})

	if got := os.Getenv("INFER_INHERITED"); got != "" {
		t.Errorf("INFER_INHERITED = %q, want empty", got)
	}
	if got := os.Getenv("INFER_BASE_URL"); got != "http://example.test/v1" {
		t.Errorf("INFER_BASE_URL = %q, want the given value", got)
	}
	if got := os.Getenv("APPTEST_OTHER"); got != "other" {
		t.Errorf("APPTEST_OTHER = %q, want it untouched", got)
	}
}
