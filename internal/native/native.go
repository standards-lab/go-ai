package native

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Valid reports whether v can be a request's Native field: empty, or a JSON object with unique
// member names.
func Valid(v jsontext.Value) bool {
	return len(v) == 0 || (v.Kind() == jsontext.KindBeginObject && v.IsValid())
}

// Has reports whether v, a Valid Native field, has a member named name.
func Has(v jsontext.Value, name string) bool {
	var members map[string]jsontext.Value
	if len(v) == 0 || json.Unmarshal(v, &members) != nil {
		return false
	}
	_, ok := members[name]
	return ok
}

// Merge returns one Native field holding the members of a and then those of b, both Valid
// Native fields, or empty when both are. A member of b named like one of a is reported as a
// *ClashError naming it, the first such name in sorted order.
func Merge(a, b jsontext.Value) (jsontext.Value, error) {
	ma, err := members(a)
	if err != nil {
		return nil, err
	}
	mb, err := members(b)
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(mb)) {
		if _, ok := ma[name]; ok {
			return nil, &ClashError{Member: name}
		}
	}
	var merged jsontext.Value
	for _, v := range []jsontext.Value{a, b} {
		inner, err := innerMembers(v)
		if err != nil {
			return nil, err
		}
		switch {
		case len(inner) == 0:
		case len(merged) == 0:
			merged = append(jsontext.Value{'{'}, inner...)
		default:
			merged = append(append(merged, ','), inner...)
		}
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return append(merged, '}'), nil
}

func members(v jsontext.Value) (map[string]jsontext.Value, error) {
	m := map[string]jsontext.Value{}
	if len(v) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(v, &m); err != nil {
		return nil, fmt.Errorf("decode native field: %w", err)
	}
	return m, nil
}

func innerMembers(v jsontext.Value) ([]byte, error) {
	if len(v) == 0 {
		return nil, nil
	}
	c := v.Clone()
	if err := c.Compact(); err != nil {
		return nil, fmt.Errorf("compact native field: %w", err)
	}
	return c[1 : len(c)-1], nil
}

// Marshal encodes body, a wire body whose Native field is embedded in its top level, as JSON. A
// Native member named like one body's typed fields set is a duplicate name, which it reports as
// a *ClashError naming the member. body's Native must be Valid, or a repeated member within it is
// reported as a clash too.
func Marshal(body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		var se *jsontext.SyntacticError
		if errors.As(err, &se) && errors.Is(se.Err, jsontext.ErrDuplicateName) {
			return nil, &ClashError{Member: se.JSONPointer.LastToken()}
		}
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return b, nil
}

// ClashError is a Native member named like one a wire body's typed fields set, which Marshal
// refuses. A format reports it as its own refusal of the member. This package can't build that
// refusal itself: the format package imports protocol, which imports this package.
type ClashError struct {
	// Member is the clashing member's name.
	Member string
}

// Error names the clashing member.
func (e *ClashError) Error() string {
	return fmt.Sprintf("native member %s clashes with a member the request sets", e.Member)
}
