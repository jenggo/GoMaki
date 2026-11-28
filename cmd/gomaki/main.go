package main

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/term"
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
	if a.password != "" && !runHeadless(a) {
		return
	}
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

// runHeadless serves HTTP without a desktop window and reports whether to
// continue into Desktop Mode.
func runHeadless(a *app) bool {
	if err := a.serveHTTP(); err != nil {
		log.Fatal(err)
	}
	desktop := waitForDesktopKey()
	a.shutdownHeadless()
	if desktop {
		// Desktop Mode runs its own server on demand; it must not try to
		// rebind the port this headless server just released.
		a.password = ""
	}
	return desktop
}

// waitForDesktopKey blocks until the user presses D (case-sensitive) to
// switch to the desktop app, or until the process is interrupted.
func waitForDesktopKey() bool {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		if state, err := term.MakeRaw(fd); err == nil {
			defer func() { _ = term.Restore(fd, state) }()
		}
	}
	keys := make(chan bool, 1)
	go func() { keys <- readDesktopKey(fd) }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case desktop := <-keys:
		return desktop
	case <-stop:
		return false
	}
}

func readDesktopKey(fd int) bool {
	if !term.IsTerminal(fd) {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if scanner.Text() == "D" {
				return true
			}
		}
		return false
	}
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return false
		}
		switch buf[0] {
		case 'D':
			return true
		case 3: // Raw mode turns Ctrl+C into a byte instead of a signal.
			return false
		}
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
