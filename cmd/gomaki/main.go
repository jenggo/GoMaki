package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const appName = "GoMaki"

func main() {
	a := &app{planIndex: -1, sessionPick: -1, settings: defaultSettings}
	a.cwd, _ = os.Getwd()
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		if a.client != nil {
			_ = a.client.Close()
		}
	})
	mygo.App.WhenReady(func() {
		a.loadSettings()
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:   appName,
			Width:   1000,
			Height:  760,
			Content: ui.View(a.view),
		})
		a.syncMenu()
		a.startup()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
