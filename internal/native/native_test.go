package native_test

import (
	"encoding/json/jsontext"
	"errors"
	"testing"

	"github.com/standards-lab/go-ai/internal/native"
)

func TestMerge(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{"", "", ""},
		{`{"a":1}`, "", `{"a":1}`},
		{"", `{"b":2}`, `{"b":2}`},
		{`{}`, `{ }`, ""},
		{` { "a" : 1 } `, `{"b":[2, 3],"c":"x"}`, `{"a":1,"b":[2,3],"c":"x"}`},
	}
	for _, tc := range cases {
		got, err := native.Merge(jsontext.Value(tc.a), jsontext.Value(tc.b))
		if err != nil || string(got) != tc.want {
			t.Errorf("Merge(%s, %s) = %s, %v; want %s", tc.a, tc.b, got, err, tc.want)
		}
	}
}

func TestMergeRefusesClash(t *testing.T) {
	_, err := native.Merge(jsontext.Value(`{"z":1,"b":1,"a":1}`), jsontext.Value(`{"z":2,"b":2}`))
	ce, ok := errors.AsType[*native.ClashError](err)
	if !ok || ce.Member != "b" {
		t.Errorf("Merge error = %v, want a *ClashError naming b", err)
	}
}
