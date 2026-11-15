package acpclient

import "testing"

func TestParseFields(t *testing.T) {
	props := map[string]any{
		"q2": map[string]any{
			"type":  "array",
			"title": "Pick some",
			"items": map[string]any{
				"type": "string",
				"enum": []any{"X", "Y"},
			},
		},
		"q1": map[string]any{
			"type":  "string",
			"title": "Choose one",
			"oneOf": []any{
				map[string]any{"const": "A", "title": "A - first"},
				map[string]any{"const": "B", "title": "B - second"},
			},
		},
		"q3": map[string]any{
			"type":  "string",
			"title": "Type anything",
		},
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
