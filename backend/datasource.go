package backend

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gioui.org/x/explorer"
	"git.sr.ht/~gioverse/skel/stream"
	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
	"github.com/fsnotify/fsnotify"
)

type InputKind uint8

const (
	KindSample InputKind = iota
	KindHeadings
)

type InputData struct {
	Kind InputKind
	Sample
	Headings []string
}

type Sample struct {
	StartTimestampNS, EndTimestampNS int64
	Series                           int
	Value                            float64
	Unit                             sensors.Unit
}

type Mode uint8

const (
	ModeNone Mode = iota
	ModeSensing
	ModeReplaying
)

type Status struct {
	Mode Mode
	Err  error
}

type Datasource struct {
	pool         *stream.MutationPool[struct{}, chan any]
	statusSource *stream.Source[Status, Status]
	watcher      *fsnotify.Watcher
	samples      chan InputData
	appCtx       context.Context
}

func NewDatasource(appCtx context.Context, mutator *stream.Mutator) (*Datasource, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed creating file watcher: %w", err)
	}
	return &Datasource{
		pool: stream.NewMutationPool[struct{}, chan any](mutator),
		statusSource: stream.NewSource(func(s Status) (Status, bool) {
			return s, true
		}),
		watcher: watcher,
		appCtx:  appCtx,
		samples: make(chan InputData, 1024),
	}, nil
}

func (d *Datasource) Status(ctx context.Context) <-chan Status {
	return d.statusSource.Stream(ctx)
}

func (d *Datasource) Samples() <-chan InputData {
	return d.samples
}

func (d *Datasource) LoadFromFile(expl *explorer.Explorer) {
	file, err := expl.ChooseFile()
	if err != nil {
		d.statusSource.Update(func(oldState Status) Status {
			oldState.Err = err
			return oldState
		})
		return
	}
	d.LoadFromStream(file)
}

func (d *Datasource) LoadFromStream(file io.ReadCloser) {
	if f, ok := file.(interface{ Name() string }); ok {
		d.watcher.Add(f.Name())
	}
	go d.readSource(file)
	d.statusSource.Update(func(oldState Status) Status {
		oldState.Mode = ModeReplaying
		return oldState
	})
}

func (d *Datasource) LaunchSensors() {
	traceReader, err := launchSensors(d.appCtx)
	if err != nil {
		d.statusSource.Update(func(oldState Status) Status {
			oldState.Err = err
			return oldState
		})
		return
	}
	go d.readSource(traceReader)
	d.statusSource.Update(func(oldState Status) Status {
		oldState.Mode = ModeSensing
		return oldState
	})
}

func runSensorsWithName(ctx context.Context, exeName string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, exeName)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed acquiring stdout pipe: %w", err)
	}
	return out, cmd.Start()
}

func launchSensors(ctx context.Context) (io.ReadCloser, error) {
	const sensorExeName = "watt-wiser-sensors"
	execPath, err := os.Executable()
	if err == nil {
		sensorExe := filepath.Join(filepath.Dir(execPath), sensorExeName)
		if runtime.GOOS == "windows" {
			sensorExe += ".exe"
		}
		log.Printf("Looking for %q", sensorExe)
		output, err := runSensorsWithName(ctx, sensorExe)
		if err == nil {
			return output, nil
		}
	}

	log.Printf("Searching path for sensors")
	sensorExe, err := exec.LookPath(sensorExeName)
	if err != nil {
		return nil, fmt.Errorf("unable to locate %q in $PATH: %w", sensorExeName, err)
	}

	output, err := runSensorsWithName(ctx, sensorExe)
	if err != nil {
		return nil, fmt.Errorf("failed launching %q: %w", sensorExe, err)
	}

	return output, nil
}

func (d *Datasource) setError(err error) {
	d.statusSource.Update(func(oldState Status) Status {
		oldState.Err = err
		return oldState
	})
}

func (d *Datasource) readSource(source io.Reader) {
	bufRead := NewLineReader(source)
	csvReader := csv.NewReader(bufRead)
	csvReader.TrimLeadingSpace = true
	headings, err := csvReader.Read()
	if err != nil {
		d.setError(err)
		return
	}
	relevantIndices := make([]int, 2, len(headings))
	relevantIndices[0] = 0
	relevantIndices[1] = 1
	relevantHeadings := make([]string, 0, len(headings))
	indexIsEnergy := map[int]bool{}
	for i, heading := range headings {
		if i == 0 {
			continue
		}
		if strings.Contains(heading, "(J)") {
			relevantIndices = append(relevantIndices, i)
			relevantHeadings = append(relevantHeadings, heading)
			indexIsEnergy[i] = true
		} else if strings.Contains(heading, "(W)") {
			relevantIndices = append(relevantIndices, i)
			relevantHeadings = append(relevantHeadings, heading)
			indexIsEnergy[i] = false
		}
	}
	d.samples <- InputData{
		Kind:     KindHeadings,
		Headings: relevantHeadings,
	}
	// Continously parse the CSV data and send it on the channel.
readLoop:
	for {
		rec, err := csvReader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				for ev := range d.watcher.Events {
					if ev.Op == fsnotify.Write {
						continue readLoop
					}
				}
			}
			log.Printf("could not read sensor data: %v", err)
			return
		}
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
			unit := sensors.Joules
			if !indexIsEnergy[i] {
				unit = sensors.Watts
			}
			d.samples <- InputData{
				Kind: KindSample,
				Sample: Sample{
					StartTimestampNS: startNs,
					EndTimestampNS:   endNs,
					Series:           i - 2,
					Value:            data,
					Unit:             unit,
				},
			}
		}
	}
}

// lineReader is a specialized reader that ensures only entire newline-delimited lines are
// read at a time. This is useful when attempting to parse a file that is being actively
// written to as a CSV, as you don't actually attempt to parse any partial lines.
type lineReader struct {
	r       *bufio.Reader
	partial []byte
}

var _ io.Reader = (*lineReader)(nil)

func NewLineReader(r io.Reader) *lineReader {
	return &lineReader{
		r: bufio.NewReader(r),
	}
}

func (l *lineReader) Read(b []byte) (int, error) {
	data, err := l.r.ReadBytes(byte('\n'))
	if err != nil {
		l.partial = append(l.partial, data...)
		return 0, io.EOF
	}
	var n int
	if len(l.partial) > 0 {
		n = copy(b, l.partial)
		l.partial = l.partial[:copy(l.partial, l.partial[n:])]
		b = b[n:]
	}
	return n + copy(b, data), nil
}