package analytics

import (
	"encoding/json"
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
