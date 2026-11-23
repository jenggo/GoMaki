package main

import (
	"fmt"
	"os"
	"time"

	"gomaki/internal/acpclient"
	"gomaki/internal/session"
)

func main() {
	dir := os.Args[1]
	id := os.Args[2]

	raw(dir, id)
	core(dir, id)
}

func raw(dir, id string) {
	c, err := acpclient.Start(dir)
	if err != nil {
		panic(err)
	}
	n, bytes := 0, 0
	done := make(chan struct{})
	go func() {
		for e := range c.Events() {
			n++
			bytes += len(e.Text)
		}
		close(done)
	}()
	t := time.Now()
	_, err = c.LoadSession(dir, id)
	fmt.Println("raw load:", time.Since(t).Round(time.Millisecond), "events:", n, "text:", bytes, "err:", err)
	<-done
}

func core(dir, id string) {
	core := session.New()
	if err := core.Start(dir); err != nil {
		panic(err)
	}
	t := time.Now()
	err := core.LoadSession(id)
	st := core.Snapshot()
	fmt.Println("core load:", time.Since(t).Round(time.Millisecond), "entries:", len(st.Transcript),
		"text:", textBytes(st), "err:", err)
}

func textBytes(s session.State) int {
	n := 0
	for _, e := range s.Transcript {
		n += len(e.Text)
	}
	return n
}
