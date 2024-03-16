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
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/x/explorer"
	"git.sr.ht/~gioverse/skel/stream"
	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
	"github.com/fsnotify/fsnotify"
)

type RWBox[T any] struct {
	t    T
	lock sync.RWMutex
}

func (r *RWBox[T]) Read(f func(*T)) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	f(&r.t)
}

func (r *RWBox[T]) Write(f func(*T)) {
	r.lock.Lock()
	defer r.lock.Unlock()
	f(&r.t)
}

type InputKind uint8

const (
	KindSample InputKind = iota
	KindHeadings
)

type InputData struct {
	Kind InputKind
	Sample
	Headings      []string
	HeadingSeries []int
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
	Mode      Mode
	SessionID string
	Err       error
}

type Datasource struct {
	pool            *stream.MutationPool[string, *RWBox[Dataset]]
	statusSource    *stream.Source[Status, Status]
	watcher         *fsnotify.Watcher
	samples         chan InputData
	internalSamples chan InputData
	appCtx          context.Context
	seriesCounter   atomic.Int32
}

func NewDatasource(appCtx context.Context, mutator *stream.Mutator) (*Datasource, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed creating file watcher: %w", err)
	}
	ds := &Datasource{
		pool: stream.NewMutationPool[string, *RWBox[Dataset]](mutator),
		statusSource: stream.NewSource(func(s Status) (Status, bool) {
			return s, true
		}),
		watcher:         watcher,
		appCtx:          appCtx,
		samples:         make(chan InputData, 1024),
		internalSamples: make(chan InputData, 1024),
	}
	return ds, nil
}

func (d *Datasource) Status(ctx context.Context) <-chan Status {
	return d.statusSource.Stream(ctx)
}

func generateSessionID() string {
	return strings.Replace(time.Now().UTC().Format("20060102150405.000000000"), ".", "", 1)
}

func sessionFileFor(sessionID string) string {
	return "watt-wiser-" + sessionID + ".csv"
}

func benchmarkFileFor(sessionID string) string {
	return "watt-wiser-" + sessionID + "-benchmarks.json"
}

func (d *Datasource) setSession(id string) {
	d.statusSource.Update(func(oldState Status) Status {
		oldState.SessionID = id
		return oldState
	})
}

func (d *Datasource) GetStatus(ctx context.Context) Status {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return <-d.Status(ctx)
}

func (d *Datasource) setError(err error) {
	log.Printf("datasource error: %v", err)
	d.statusSource.Update(func(oldState Status) Status {
		oldState.Err = err
		return oldState
	})
}

func (d *Datasource) recordSession(sessionID string) *stream.Mutation[*RWBox[Dataset]] {
	box, _ := stream.Mutate(d.pool, sessionID, func(ctx context.Context) (values <-chan *RWBox[Dataset]) {
		out := make(chan *RWBox[Dataset], 1)
		go func() {
			defer close(out)
			var data RWBox[Dataset]
			// Emit our boxed dataset immediately.
			out <- &data
			var sessionFile *os.File
			var sessionWriter *bufio.Writer
			var csvWriter *csv.Writer
			flushAll := func() {
				csvWriter.Flush()
				err := sessionWriter.Flush()
				err = errors.Join(err, sessionFile.Close())
				if err != nil {
					d.setError(err)
				}
			}
			headings := []string{"start (ns)", "end (ns)"}
			seriesIDToHeading := map[int]int{}
			for {
				select {
				case <-ctx.Done():
					flushAll()
					return
				case sample := <-d.internalSamples:
					if sample.Kind == KindHeadings {
						data.Write(func(d *Dataset) {
							d.SetHeadings(sample.Headings, sample.HeadingSeries)
						})
						newSessionID := generateSessionID()
						if sessionFile != nil {
							flushAll()
						}
						var err error
						sessionFile, err = os.Create(sessionFileFor(newSessionID))
						if err != nil {
							d.setError(err)
							return
						}
						sessionWriter = bufio.NewWriter(sessionFile)
						csvWriter = csv.NewWriter(sessionWriter)
						d.setSession(newSessionID)
						for sampleHeadingIdx, heading := range sample.Headings {
							series := sample.HeadingSeries[sampleHeadingIdx]
							localHeadingIdx := len(headings)
							headings = append(headings, heading)
							seriesIDToHeading[series] = localHeadingIdx
						}
						if err := csvWriter.Write(headings); err != nil {
							d.setError(err)
							return
						}
					} else {
						data.Write(func(d *Dataset) {
							d.Insert(sample.Sample)
						})
						start := strconv.FormatInt(sample.StartTimestampNS, 10)
						end := strconv.FormatInt(sample.EndTimestampNS, 10)
						position := seriesIDToHeading[sample.Series]
						val := strconv.FormatFloat(sample.Value, 'f', -1, 64)
						record := make([]string, len(headings))
						record[0] = start
						record[1] = end
						record[position] = val
						if err := csvWriter.Write(record); err != nil {
							d.setError(err)
							return
						}
					}
				}
			}
		}()
		return out
	})
	return box
}

func (d *Datasource) LoadFromFile(expl *explorer.Explorer) {
	file, err := expl.ChooseFile()
	if err != nil {
		d.setError(err)
		return
	}
	d.LoadFromStream(file)
}

func (d *Datasource) LoadFromStream(files ...io.ReadCloser) {
	//TODO: Spawn a new session for this files set
	for _, file := range files {
		if f, ok := file.(interface{ Name() string }); ok {
			d.watcher.Add(f.Name())
		}
		go d.readSource(file)
		d.statusSource.Update(func(oldState Status) Status {
			oldState.Mode = ModeReplaying
			return oldState
		})
	}
}

func (d *Datasource) LaunchSensors() {
	//TODO: Spawn a new session for this sensor run
	traceReader, err := launchSensors(d.appCtx)
	if err != nil {
		d.setError(err)
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
	headingSeries := make([]int, 0, len(headings))
	indexIsEnergy := map[int]bool{}
	for i, heading := range headings {
		if i == 0 {
			continue
		}
		joules := strings.Contains(heading, "(J)")
		watts := strings.Contains(heading, "(W)")
		if joules || watts {
			relevantIndices = append(relevantIndices, i)
			relevantHeadings = append(relevantHeadings, heading)
			headingSeries = append(headingSeries, int(d.seriesCounter.Add(1)))
			if joules {
				indexIsEnergy[i] = true
			} else if watts {
				indexIsEnergy[i] = false
			}
		}
	}
	d.internalSamples <- InputData{
		Kind:          KindHeadings,
		Headings:      relevantHeadings,
		HeadingSeries: headingSeries,
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
			record := strings.TrimSpace(rec[relevantIndices[i]])
			if len(record) < 1 {
				// Skip null cells.
				continue
			}
			data, err := strconv.ParseFloat(record, 64)
			if err != nil {
				log.Printf("failed parsing data[%d]=%q: %v", i, rec[i], err)
				continue
			}
			unit := sensors.Joules
			if !indexIsEnergy[i] {
				unit = sensors.Watts
			}
			d.internalSamples <- InputData{
				Kind: KindSample,
				Sample: Sample{
					StartTimestampNS: startNs,
					EndTimestampNS:   endNs,
					Series:           headingSeries[i-2],
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
