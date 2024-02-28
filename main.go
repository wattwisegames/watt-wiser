package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/x/explorer"
	"git.sr.ht/~gioverse/skel/stream"
	"git.sr.ht/~whereswaldon/watt-wiser/backend"
)

func main() {
	var traceInto string
	flag.StringVar(&traceInto, "trace", "", "collect a go runtime trace into the given file")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `%[1]s: visualize a csv energy trace file
Usage:

 %[1]s [flags] <file>

OR

 watt-wiser-sensors | %[1]s [flags]

Flags:
`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	var f *os.File
	if traceInto != "" {
		pprof.StartCPUProfile(io.Discard)
		var err error
		f, err = os.Create(traceInto)
		if err != nil {
			log.Printf("failed creating trace file: %v", err)
		} else {
			trace.Start(f)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	mutator := stream.NewMutator(ctx, time.Second)

	bundle, err := backend.NewBundle(ctx, mutator)
	if err != nil {
		log.Fatalf("unable to initialize application backend: %v", err)
	}
	go func() {
		w := app.NewWindow(app.Title("Watt Wiser"))
		expl := explorer.NewExplorer(w)
		if flag.NArg() > 0 {
			f, err := os.Open(flag.Arg(0))
			if err != nil {
				log.Printf("failed opening %q, falling back to stdin: %v", flag.Arg(0), err)
			}
			bundle.Datasource.LoadFromStream(f)
		}
		go func() {
			err := loop(w, expl, bundle)
			if traceInto != "" {
				trace.Stop()
				f.Close()
				pprof.StopCPUProfile()
			}
			exitStatus := 0
			if err != nil {
				exitStatus = 1
				log.Println(err)
			}
			err = mutator.Shutdown()
			if err != nil {
				exitStatus = 1
				log.Println(err)
			}
			cancel()
			os.Exit(exitStatus)
		}()
	}()

	app.Main()
}

// loop runs the top-level application event loop, connecting a UI instance to sources of data
// and ensuring that the UI is notified of new data.
func loop(w *app.Window, expl *explorer.Explorer, bundle backend.Bundle) error {
	var ops op.Ops
	var dataMutex sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws := backend.NewWindowState(ctx, bundle, w)

	ui := NewUI(ws, expl)
	go func() {
		for sample := range bundle.Datasource.Samples() {
			func() {
				dataMutex.Lock()
				defer dataMutex.Unlock()
				ui.Insert(sample)
			}()
			w.Invalidate()
		}
	}()
	for {
		ev := w.NextEvent()
		expl.ListenEvents(ev)
		switch ev := ev.(type) {
		case app.DestroyEvent:
			return ev.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, ev)
			func() {
				dataMutex.Lock()
				defer dataMutex.Unlock()
				ui.Layout(gtx)
			}()
			ev.Frame(gtx.Ops)
		}
	}
}
