package acpclient

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestParseFields(t *testing.T) {
	props := map[string]acp.ElicitationPropertySchema{
		"q2": {Array: &acp.ElicitationPropertySchemaArray{
			Title: new("Pick some"),
			Items: acp.MultiSelectItems{String: &acp.MultiSelectItemsString{
				Enum: []string{"X", "Y"},
			}},
		}},
		"q1": {String: &acp.ElicitationPropertySchemaString{
			Title: new("Choose one"),
			OneOf: []acp.EnumOption{
				{Const: "A", Title: "A - first"},
				{Const: "B", Title: "B - second"},
			},
		}},
		"q3": {String: &acp.ElicitationPropertySchemaString{
			Title: new("Type anything"),
		}},
	}

	fields := parseFields(props)
	if len(fields) != 3 {
		t.Fatalf("fields = %d", len(fields))
	}
	if fields[0].Name != "q1" || fields[1].Name != "q2" || fields[2].Name != "q3" {
		t.Fatalf("order = %q %q %q", fields[0].Name, fields[1].Name, fields[2].Name)
	}
	if got := fields[0].Options; len(got) != 2 || got[0].Value != "A" || got[0].Label != "A - first" {
		t.Fatalf("q1 options = %+v", got)
	}
	if !fields[1].Multi || len(fields[1].Options) != 2 || fields[1].Options[1].Value != "Y" {
		t.Fatalf("q2 = %+v", fields[1])
	}
	if len(fields[2].Options) != 0 || fields[2].Multi {
		t.Fatalf("q3 = %+v", fields[2])
	}
}
