package main

import "testing"

func TestMenuItems(t *testing.T) {
	a := newTestApp()
	a.models = []string{"model/a", "model/b"}
	a.model = "model/b"
	a.modes = []string{"build", "plan"}
	a.mode = "plan"

	m := a.menu()
	if item := m.ItemByID("new-session"); item == nil || !item.Disabled {
		t.Fatalf("new session item = %+v", item)
	}
	models := m.ItemByID("model-menu")
	if models == nil || len(models.Submenu) != 2 {
		t.Fatalf("model items = %+v", models)
	}
	if !models.Submenu[1].Checked || models.Submenu[0].Checked {
		t.Fatalf("model checks = %+v %+v", models.Submenu[0], models.Submenu[1])
	}
	modes := m.ItemByID("mode-menu")
	if modes == nil || len(modes.Submenu) != 2 {
		t.Fatalf("mode items = %+v", modes)
	}
	if !modes.Submenu[1].Checked || modes.Submenu[1].Label != "Plan" {
		t.Fatalf("mode checks = %+v", modes.Submenu[1])
	}
}
