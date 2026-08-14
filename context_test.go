package analytics

import (
	"encoding/json"
	"testing"
)

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

func TestContextMarshalJSONGroupID(t *testing.T) {
	tests := []struct {
		name     string
		context  Context
		expected string
	}{
		{
			name:     "typed group ID",
			context:  Context{GroupID: "group-123"},
			expected: `{"groupId":"group-123"}`,
		},
		{
			name: "typed group ID takes precedence over extra",
			context: Context{
				GroupID: "typed-group",
				Extra:   map[string]interface{}{"groupId": "extra-group"},
			},
			expected: `{"groupId":"typed-group"}`,
		},
		{
			name: "extra group ID remains supported",
			context: Context{
				Extra: map[string]interface{}{"groupId": "extra-group"},
			},
			expected: `{"groupId":"extra-group"}`,
		},
		{
			name:     "empty group ID is omitted",
			context:  Context{},
			expected: `{}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b, err := json.Marshal(test.context)
			if err != nil {
				t.Fatal("marshalling context object failed:", err)
			}
			if actual := string(b); actual != test.expected {
				t.Errorf("invalid marshaled representation: expected %s, received %s", test.expected, actual)
			}
		})
	}
}
