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
	if m.ItemByID("mode-menu") != nil {
		t.Fatal("mode menu should be gone; the status bar switches mode")
	}
	if m.ItemByID("about") == nil {
		t.Fatal("about item missing")
	}
}
