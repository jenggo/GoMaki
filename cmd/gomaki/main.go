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
	password := flag.String("password", "", "password for HTTP Mode")
	listen := flag.String("listen", "", "address to bind in HTTP Mode")
	port := flag.Int("port", 0, "port for HTTP Mode")
	flag.Parse()
	a := newApp()
	a.continueLast = *continueLast
	a.password = *password
	a.listen = *listen
	a.port = *port
	a.cwd, _ = os.Getwd()
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.busy && !a.quitting {
			e.PreventDefault()
			a.confirmQuit()
			return
		}
		a.stopHTTP()
		if a.core != nil {
			_ = a.core.Close()
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
			log.Printf("window icon: %v", err)
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
