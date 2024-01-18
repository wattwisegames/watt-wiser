package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"strings"
	"sync"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/x/explorer"
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
			err := loop(w, watcher, samplesChan)
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

func runSensorsWithName(exeName string) (*exec.Cmd, io.ReadCloser, error) {
	cmd := exec.Command(exeName)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed acquiring stdout pipe: %w", err)
	}
	return cmd, out, cmd.Start()
}

func launchSensors() (*exec.Cmd, io.ReadCloser, error) {
	const sensorExeName = "watt-wiser-sensors"
	execPath, err := os.Executable()
	if err == nil {
		sensorExe := filepath.Join(filepath.Dir(execPath), sensorExeName)
		if runtime.GOOS == "windows" {
			sensorExe += ".exe"
		}
		log.Printf("Looking for %q", sensorExe)
		cmd, output, err := runSensorsWithName(sensorExe)
		if err == nil {
			return cmd, output, nil
		}
	}

	log.Printf("Searching path for sensors")
	sensorExe, err := exec.LookPath(sensorExeName)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to locate %q in $PATH: %w", sensorExeName, err)
	}

	cmd, output, err := runSensorsWithName(sensorExe)
	if err != nil {
		return nil, nil, fmt.Errorf("failed launching %q: %w", sensorExe, err)
	}

	return cmd, output, nil
}

// loop runs the top-level application event loop, connecting a UI instance to sources of data
// and ensuring that the UI is notified of new data.
func loop(w *app.Window, watcher *fsnotify.Watcher, samples chan inputData) error {
	expl := explorer.NewExplorer(w)
	var ops op.Ops
	var dataMutex sync.Mutex

	onClose := func() {}
	defer func() {
		onClose()
	}()

	ui := NewUI(w)
	go func() {
		for sample := range samples {
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
		case system.DestroyEvent:
			return ev.Err
		case system.FrameEvent:
			gtx := layout.NewContext(&ops, ev)
			func() {
				dataMutex.Lock()
				defer dataMutex.Unlock()
				for {
					ev, ok := ui.Update(gtx)
					if !ok {
						break
					}
					switch ev.(type) {
					case LoadFileRequest:
						file, err := expl.ChooseFile()
						if err != nil {
							ui.sensorsErr = err.Error()
						} else {
							if f, ok := file.(interface{ Name() string }); ok {
								watcher.Add(f.Name())
							}
							go func() {
								readSource(file, watcher, samples)
							}()
						}
					case LaunchSensorsRequest:
						cmd, traceReader, err := launchSensors()
						if err != nil {
							ui.sensorsErr = err.Error()
						} else {
							onClose = func() {
								cmd.Process.Kill()
							}
							go func() {
								readSource(traceReader, watcher, samples)
							}()
						}
					}
				}
				ui.Layout(gtx)
			}()
			ev.Frame(gtx.Ops)
		}
	}
}
