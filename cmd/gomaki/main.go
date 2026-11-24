package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	appName = "GoMaki"

	usageContinue = "continue the most recent session"
	usagePassword = "password for HTTP Mode"
	usageListen   = "address to bind in HTTP Mode"
	usagePort     = "port for HTTP Mode"
)

//go:embed icon.png
var appIcon []byte

//go:embed icon-128.png
var appIconSmall []byte

func main() {
	var continueLast bool
	var password, listen string
	var port int
	flag.BoolVar(&continueLast, "c", false, usageContinue)
	flag.BoolVar(&continueLast, "continue", false, usageContinue)
	flag.StringVar(&password, "p", "", usagePassword)
	flag.StringVar(&password, "password", "", usagePassword)
	flag.StringVar(&listen, "l", "", usageListen)
	flag.StringVar(&listen, "listen", "", usageListen)
	flag.IntVar(&port, "P", 0, usagePort)
	flag.IntVar(&port, "port", 0, usagePort)
	flag.Usage = usage
	flag.Parse()
	a := newApp()
	a.continueLast = continueLast
	a.password = password
	a.listen = listen
	a.port = port
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

func usage() {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "Usage of %s:\n", os.Args[0])
	for _, f := range [][2]string{
		{"--continue / -c", usageContinue},
		{"--password / -p string", usagePassword},
		{"--listen / -l string", usageListen},
		{"--port / -P int", usagePort},
		{"--help / -h", "show this help"},
	} {
		_, _ = fmt.Fprintf(out, "  %-22s  %s\n", f[0], f[1])
	}
}
