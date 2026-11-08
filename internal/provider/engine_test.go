package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTfValue(t *testing.T) {
	cases := []struct {
		name string
		f    field
		row  map[string]any
		want attr.Value
	}{
		{"string", str("a"), map[string]any{"a": "v"}, types.StringValue("v")},
		{"missing string is null", str("a"), map[string]any{}, types.StringNull()},
		{"number printed as string", str("a"), map[string]any{"a": float64(8080)}, types.StringValue("8080")},
		{"bool on", flag("a"), map[string]any{"a": "on"}, types.BoolValue(true)},
		{"bool off", flag("a"), map[string]any{"a": "off"}, types.BoolValue(false)},
		{"bool custom words", flag("a").words("1", "0"), map[string]any{"a": "1"}, types.BoolValue(true)},
		{"bool from json number 0", flag("a").words("1", "0"), map[string]any{"a": float64(0)}, types.BoolValue(false)},
		{"missing bool is null", flag("a"), map[string]any{}, types.BoolNull()},
		{"int from string", num("a"), map[string]any{"a": "120"}, types.Int64Value(120)},
		{"int from json number", num("a"), map[string]any{"a": float64(1024)}, types.Int64Value(1024)},
		{"unparsable int is null", num("a"), map[string]any{"a": "--"}, types.Int64Null()},
		{"read name mapping", str("ip").rd("ipaddr"), map[string]any{"ipaddr": "1.2.3.4"}, types.StringValue("1.2.3.4")},
	}
	for _, c := range cases {
		if got := tfValue(c.f, c.row); !got.Equal(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTfValueStringList(t *testing.T) {
	list := func(s ...string) attr.Value {
		els := make([]attr.Value, len(s))
		for i, v := range s {
			els[i] = types.StringValue(v)
		}
		return types.ListValueMust(types.StringType, els)
	}
	cases := []struct {
		name string
		v    any
		want attr.Value
	}{
		{"array", []any{"a", "b"}, list("a", "b")},
		{"bare string is one member", "LAN1", list("LAN1")},
		{"empty table reported as object", map[string]any{}, list()},
		{"empty-string selection is empty", []any{""}, list()},
		{"empty string", "", list()},
	}
	for _, c := range cases {
		if got := tfValue(strs("l"), map[string]any{"l": c.v}); !got.Equal(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRowParams(t *testing.T) {
	row := map[string]any{"ip": "192.168.0.5", "interface": "LAN1", "vlanid": "7"}
	plain := &typedResource{s: &spec{}}
	if p := plain.rowParams(row, 3); p["key"] != "key-3" || p["index"] != "3" || len(p) != 2 {
		t.Errorf("positional params = %v", p)
	}
	keyed := &typedResource{s: &spec{rowKey: "ip", rowExtra: "interface"}}
	p := keyed.rowParams(row, 1)
	if p["key"] != "192.168.0.5" || p["ip"] != "192.168.0.5" || p["extraKey"] != "LAN1" || p["index"] != "1" {
		t.Errorf("keyed params = %v", p)
	}
}

func TestSpecsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range specs {
		if seen[s.typeName] {
			t.Errorf("duplicate resource %q", s.typeName)
		}
		seen[s.typeName] = true
		if s.endpoint == "" || len(s.fields) == 0 {
			t.Errorf("%s: missing endpoint or fields", s.typeName)
		}
		names := map[string]bool{}
		for _, f := range s.fields {
			if names[f.name] {
				t.Errorf("%s: duplicate field %q", s.typeName, f.name)
			}
			names[f.name] = true
			for _, r := range f.name {
				if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
					t.Errorf("%s.%s: Terraform attribute names must be lowercase snake_case", s.typeName, f.name)
					break
				}
			}
		}
		if s.isList() && !names[s.key] {
			t.Errorf("%s: key %q is not a field", s.typeName, s.key)
		}
	}
}
