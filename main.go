package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"sync"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget/material"
	"github.com/fsnotify/fsnotify"
)

type inputKind uint8

const (
	kindSample inputKind = iota
	kindHeadings
)

type inputData struct {
	Kind inputKind
	Sample
	Headings []string
}

type Sample struct {
	StartTimestampNS, EndTimestampNS int64
	Data                             []float64
}

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
	go func() {
		var source io.Reader = os.Stdin
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Fatalf("failed building file watcher: %v", err)
		}
		if flag.NArg() > 0 {
			f, err := os.Open(flag.Arg(0))
			if err != nil {
				log.Printf("failed opening %q, falling back to stdin: %v", flag.Arg(0), err)
			}
			defer f.Close()
			source = f
			watcher.Add(f.Name())
		}

		samplesChan := make(chan inputData, 1024)
		w := app.NewWindow(app.Title("Watt Wiser"))
		go func() {
			err := loop(w, samplesChan)
			if traceInto != "" {
				trace.Stop()
				f.Close()
				pprof.StopCPUProfile()
			}
			if err != nil {
				log.Fatal(err)
			}
			os.Exit(0)
		}()

		readSource(source, watcher, samplesChan)

	}()

	app.Main()
}

func readSource(source io.Reader, watcher *fsnotify.Watcher, samplesChan chan inputData) {
	bufRead := NewLineReader(source)
	csvReader := csv.NewReader(bufRead)
	csvReader.TrimLeadingSpace = true
	headings, err := csvReader.Read()
	if err != nil {
		log.Fatalf("could not read csv headings: %v", err)
	}
	relevantIndices := make([]int, 2, len(headings))
	relevantIndices[0] = 0
	relevantIndices[1] = 1
	relevantHeadings := make([]string, 0, len(headings))
	for i, heading := range headings {
		if i == 0 {
			continue
		}
		if strings.Contains(heading, "(J)") {
			relevantIndices = append(relevantIndices, i)
			relevantHeadings = append(relevantHeadings, heading)
		}
	}
	samplesChan <- inputData{
		Kind:     kindHeadings,
		Headings: relevantHeadings,
	}
	// Continously parse the CSV data and send it on the channel.
readLoop:
	for {
		rec, err := csvReader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				for ev := range watcher.Events {
					if ev.Op == fsnotify.Write {
						continue readLoop
					}
				}
			}
			log.Printf("could not read sensor data: %v", err)
			return
		}
		samples := make([]float64, len(relevantIndices)-2)
		startNs, err := strconv.ParseInt(rec[0], 10, 64)
		if err != nil {
			log.Printf("failed parsing timestamp: %v", err)
			continue
		}
		endNs, err := strconv.ParseInt(rec[1], 10, 64)
		if err != nil {
			log.Printf("failed parsing timestamp: %v", err)
			continue
		}
		for i := 2; i < len(relevantIndices); i++ {
			data, err := strconv.ParseFloat(rec[relevantIndices[i]], 64)
			if err != nil {
				log.Printf("failed parsing data[%d]=%q: %v", i, rec[i], err)
				continue
			}
			samples[i-2] = data
		}
		samplesChan <- inputData{
			Kind: kindSample,
			Sample: Sample{
				StartTimestampNS: startNs,
				EndTimestampNS:   endNs,
				Data:             samples,
			},
		}
	}
}

type (
	C = layout.Context
	D = layout.Dimensions
)

func loop(w *app.Window, samples chan inputData) error {
	var dataMutex sync.Mutex
	var data ChartData
	var ops op.Ops
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	go func() {
		for sample := range samples {
			func() {
				dataMutex.Lock()
				defer dataMutex.Unlock()
				switch sample.Kind {
				case kindHeadings:
					data.Headings = sample.Headings
				case kindSample:
					data.Insert(sample.Sample)
				}
			}()
			w.Invalidate()
		}
	}()
	for {
		switch ev := w.NextEvent().(type) {
		case system.DestroyEvent:
			return ev.Err
		case system.FrameEvent:
			gtx := layout.NewContext(&ops, ev)
			if data.Initialized() {
				func() {
					dataMutex.Lock()
					defer dataMutex.Unlock()
					data.Layout(gtx, th)
				}()
			} else {
				l := material.Body1(th, "No data yet.")
				layout.Center.Layout(gtx, l.Layout)
			}
			ev.Frame(gtx.Ops)
		}
	}
}
