package main

import "github.com/egoist/mygo"

// menu builds the window's menu bar: the actions, the editing commands of
// the focused field, and the live model and mode choices.
func (a *app) menu() *mygo.Menu {
	available := a.client != nil
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "File", Submenu: []*mygo.MenuItem{
			{ID: "new-session", Label: "New Session…", Accelerator: "CmdOrCtrl+N", Disabled: !available,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.pickDirectory() }},
			{ID: "open-session", Label: "Open Session…", Accelerator: "CmdOrCtrl+O", Disabled: !available,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.openSessions() }},
			{ID: "attach", Label: "Attach Image or File…", Accelerator: "CmdOrCtrl+Shift+A", Disabled: !available,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.attachFile() }},
			{ID: "reload", Label: "Reload", Accelerator: "CmdOrCtrl+R",
				Click: func(*mygo.MenuItem, *mygo.Window) { a.reload() }},
			{ID: "running", Label: "Running Tasks…", Accelerator: "CmdOrCtrl+Shift+B",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					a.runningOpen = true
					a.win.Invalidate()
				}},
			{Label: "Reload Commands", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.loadCommands()
				a.win.Invalidate()
			}},
			mygo.Separator(),
			{ID: "settings", Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
				a.settingsOpen = true
				a.win.Invalidate()
			}},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{ID: "model-menu", Label: "Model", Submenu: a.modelItems()},
		{Label: "Help", Submenu: []*mygo.MenuItem{
			{ID: "about", Label: "About GoMaki", Click: func(*mygo.MenuItem, *mygo.Window) { a.showAbout() }},
		}},
	})
}

// syncMenu rebuilds the menu bar outside the frame that changed the
// models or modes. Replacing the GTK menu bar during a draw destroys a
// widget GTK is still painting, so the rebuild is queued and coalesced.
func (a *app) syncMenu() {
	if a.win == nil || a.menuQueued {
		return
	}
	a.menuQueued = true
	a.win.Update(func() {
		a.menuQueued = false
		a.win.SetMenu(a.menu())
	})
}

func (a *app) modelItems() []*mygo.MenuItem {
	if len(a.models) == 0 {
		return []*mygo.MenuItem{{Label: "No models available", Disabled: true}}
	}
	items := make([]*mygo.MenuItem, 0, len(a.models))
	for _, model := range a.models {
		items = append(items, &mygo.MenuItem{
			Label:   model,
			Type:    mygo.MenuItemRadio,
			Checked: model == a.model,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.switchModel(model)
			},
		})
	}
	return items
}
