package analytics

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestInheritedContextValuesAreIndependent(t *testing.T) {
	defaults := &Context{
		Traits: Traits{"profile": map[string]string{"plan": "free"}},
		Extra:  map[string]interface{}{"tags": []string{"default"}},
	}
	c := &client{Config: Config{DefaultContext: defaults}}
	first := c.makeContext(nil)
	queued := c.makeContext(nil)
	first.Traits["profile"].(map[string]string)["plan"] = "changed"
	first.Extra["tags"].([]string)[0] = "changed"

	for _, ctx := range []*Context{defaults, queued, c.makeContext(nil)} {
		if !reflect.DeepEqual(ctx.Traits["profile"], map[string]string{"plan": "free"}) ||
			!reflect.DeepEqual(ctx.Extra["tags"], []string{"default"}) {
			t.Fatalf("mutating one event changed another context: %#v", ctx)
		}
	}
}

type defaultContextLookup struct {
	Key    *string
	Values map[*string]string
	Parent *defaultContextLookup `json:"-"`
}

func (lookup *defaultContextLookup) MarshalJSON() ([]byte, error) {
	return json.Marshal(lookup.Values[lookup.Key])
}

func TestInheritedContextPreservesSharedReferences(t *testing.T) {
	key := "lookup"
	lookup := &defaultContextLookup{Key: &key, Values: map[*string]string{&key: "found"}}
	lookup.Parent = lookup
	c := &client{Config: Config{DefaultContext: &Context{
		Traits: Traits{"key": &key},
		Extra:  map[string]interface{}{"lookup": lookup},
	}}}
	ctx := c.makeContext(nil)
	copied := ctx.Extra["lookup"].(*defaultContextLookup)
	if copied == lookup || copied.Parent != copied || copied.Key == &key || copied.Key != ctx.Traits["key"] {
		t.Fatal("default context copy did not preserve shared references within the event")
	}
	data, err := json.Marshal(copied)
	if err != nil || string(data) != `"found"` {
		t.Fatalf("custom JSON lookup: got %s, %v; want %q", data, err, "found")
	}
}

func TestInheritedContextReportsJSONCycles(t *testing.T) {
	cyclicMap := map[string]interface{}{}
	cyclicMap["self"] = cyclicMap
	cyclicSlice := make([]interface{}, 1)
	cyclicSlice[0] = cyclicSlice
	for _, value := range []interface{}{cyclicMap, cyclicSlice} {
		c := &client{Config: Config{DefaultContext: &Context{
			Extra: map[string]interface{}{"cycle": value},
		}}}
		_, err := json.Marshal(c.makeContext(nil))
		var unsupported *json.UnsupportedValueError
		if !errors.As(err, &unsupported) {
			t.Errorf("cycle in %T returned %v, want a JSON UnsupportedValueError", value, err)
		}
	}
}

func TestContextMarshalJSONLibrary(t *testing.T) {
	c := Context{
		Library: LibraryInfo{
			Name: "testing",
		},
	}

	if b, err := json.Marshal(c); err != nil {
		t.Error("marshalling context object failed:", err)

	} else if s := string(b); s != `{"library":{"name":"testing"}}` {
		t.Error("invalid marshaled representation of context:", s)
	}
}

func TestContextMarshalJSONExtra(t *testing.T) {
	c := Context{
		Extra: map[string]interface{}{
			"answer": 42,
		},
	}

	if b, err := json.Marshal(c); err != nil {
		t.Error("marshalling context object failed:", err)

	} else if s := string(b); s != `{"answer":42}` {
		t.Error("invalid marshaled representation of context:", s)
	}
}
