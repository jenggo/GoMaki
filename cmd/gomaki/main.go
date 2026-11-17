package main

import (
	_ "embed"
	"flag"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const appName = "GoMaki"

//go:embed icon.png
var appIcon []byte

//go:embed icon-128.png
var appIconSmall []byte

func main() {
	continueLast := flag.Bool("c", false, "continue the most recent session")
	flag.Parse()
	a := newApp()
	a.continueLast = *continueLast
	a.cwd, _ = os.Getwd()
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.busy && !a.quitting {
			e.PreventDefault()
			a.confirmQuit()
			return
		}
		if a.client != nil {
			_ = a.client.Close()
		}
	})
	mygo.App.WhenReady(func() {
		a.loadSettings()
		if err := mygo.App.Dock.SetIcon(appIconSmall); err != nil {
			log.Printf("set app icon: %v", err)
		}
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:   appName,
			Width:   1000,
			Height:  760,
			Content: ui.View(a.view),
		})
		if err := a.win.SetIcon(appIcon); err != nil {
			log.Printf("set window icon: %v", err)
		}
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.busy && !a.quitting {
				e.PreventDefault()
				a.confirmQuit()
			}
		})
		a.syncMenu()
		a.startup()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
