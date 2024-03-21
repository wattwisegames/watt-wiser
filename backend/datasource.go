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

type Session struct {
	ID   string
	Data Dataset
	Mode Mode
	Err  error
}

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

type Datasource struct {
	pool          *stream.MutationPool[string, Session]
	watcher       *fsnotify.Watcher
	appCtx        context.Context
	seriesCounter atomic.Int32
}

func NewDatasource(appCtx context.Context, mutator *stream.Mutator) (*Datasource, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed creating file watcher: %w", err)
	}
	ds := &Datasource{
		pool:    stream.NewMutationPool[string, Session](mutator),
		watcher: watcher,
		appCtx:  appCtx,
	}
	return ds, nil
}

func (d *Datasource) SessionStream(ctx context.Context) <-chan map[string]*stream.Mutation[Session] {
	return d.pool.Stream(ctx)
}

func (d *Datasource) getMutation(ctx context.Context, sessionID string) *stream.Mutation[Session] {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return (<-d.SessionStream(ctx))[sessionID]
}

func (d *Datasource) StreamSession(ctx context.Context, sessionID string) <-chan Session {
	return d.getMutation(ctx, sessionID).Stream(ctx)
}

func (d *Datasource) SensingSession(ctx context.Context) Session {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return <-stream.Filter(d.pool.Stream(ctx), func(mutations map[string]*stream.Mutation[Session]) (Session, bool) {
		for _, m := range mutations {
			subCtx, cancel := context.WithCancel(ctx)
			session := <-m.Stream(subCtx)
			cancel()
			if session.Mode == ModeSensing {
				return session, true
			}
		}
		return Session{}, false
	})
}

func (d *Datasource) SensingSessionStream(ctx context.Context) <-chan Session {
	return stream.Multiplex(d.pool.Stream(ctx), func(ctx context.Context, state string, mutations map[string]*stream.Mutation[Session]) (<-chan Session, string) {
		for _, m := range mutations {
			subCtx, cancel := context.WithCancel(ctx)
			session := <-m.Stream(subCtx)
			cancel()
			if session.Mode == ModeSensing {
				if session.ID == state {
					return nil, state
				}
				state = session.ID
				return m.Stream(ctx), state
			}
		}
		return nil, state
	})
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

func (d *Datasource) recordSession(sessionID string, mode Mode, files ...io.ReadCloser) *stream.Mutation[Session] {
	box, _ := stream.Mutate(d.pool, sessionID, func(ctx context.Context) (values <-chan Session) {
		out := make(chan Session, 1)
		go func() {
			defer close(out)
			session := Session{
				ID:   sessionID,
				Data: Dataset{},
				Mode: mode,
				Err:  nil,
			}
			// Emit our boxed dataset immediately.
			out <- session

			rawSamples := make(chan InputData, 1024)
			for _, file := range files {
				if f, ok := file.(interface{ Name() string }); ok {
					d.watcher.Add(f.Name())
				}
				go d.readSource(file, rawSamples)
			}

			var sessionFile *os.File
			var sessionWriter *bufio.Writer
			var csvWriter *csv.Writer
			var err error
			if mode == ModeSensing {
				sessionFile, err = os.Create(sessionFileFor(sessionID))
				if err != nil {
					session.Err = err
					out <- session
					return
				}
				sessionWriter = bufio.NewWriter(sessionFile)
				csvWriter = csv.NewWriter(sessionWriter)
			}
			flushAll := func() {
				if mode == ModeSensing {
					csvWriter.Flush()
					err := sessionWriter.Flush()
					err = errors.Join(err, sessionFile.Close())
					if err != nil {
						session.Err = err
						out <- session
					}
				}
			}
			headings := []string{"start (ns)", "end (ns)"}
			seriesIDToHeading := map[int]int{}
			seriesIDToSeries := map[int]int{}
			for {
				select {
				case <-ctx.Done():
					flushAll()
					return
				case sample := <-rawSamples:
					if sample.Kind == KindHeadings {
						for sampleHeadingIdx, heading := range sample.Headings {
							seriesID := sample.HeadingSeries[sampleHeadingIdx]
							localHeadingIdx := len(headings)
							headings = append(headings, heading)
							seriesIDToHeading[seriesID] = localHeadingIdx
							seriesIDToSeries[seriesID] = localHeadingIdx - 2
							session.Data = append(session.Data, NewSeries(heading))
						}
						if mode == ModeSensing {
							if err := csvWriter.Write(headings); err != nil {
								session.Err = err
								out <- session
								return
							}
						}
					} else {
						// We know the series are writable.
						session.Data[seriesIDToSeries[sample.Series]].(WritableDataSeries).Insert(sample.Sample)
						if mode == ModeSensing {
							start := strconv.FormatInt(sample.StartTimestampNS, 10)
							end := strconv.FormatInt(sample.EndTimestampNS, 10)
							position := seriesIDToHeading[sample.Series]
							val := strconv.FormatFloat(sample.Value, 'f', -1, 64)
							record := make([]string, len(headings))
							record[0] = start
							record[1] = end
							record[position] = val
							if err := csvWriter.Write(record); err != nil {
								session.Err = err
								out <- session
								return
							}
						}
					}
					out <- session
				}
			}
		}()
		return out
	})
	return box
}

func (d *Datasource) LoadFromFile(expl *explorer.Explorer) (string, error) {
	file, err := expl.ChooseFile()
	if err != nil {
		return "", err
	}
	return d.LoadFromStream(ModeReplaying, file), nil
}

func (d *Datasource) LoadFromStream(mode Mode, files ...io.ReadCloser) string {
	id := generateSessionID()
	return d.LoadFromStreamWithID(id, mode, files...)
}

func (d *Datasource) LoadFromStreamWithID(sessionID string, mode Mode, files ...io.ReadCloser) string {
	d.recordSession(sessionID, mode, files...)
	return sessionID
}

func (d *Datasource) LaunchSensors() (string, error) {
	traceReader, err := launchSensors(d.appCtx)
	if err != nil {
		return "", err
	}
	id := generateSessionID()
	d.recordSession(id, ModeSensing, traceReader)
	return id, nil
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

func (d *Datasource) readSource(source io.Reader, samplesChan chan InputData) {
	bufRead := NewLineReader(source)
	csvReader := csv.NewReader(bufRead)
	csvReader.TrimLeadingSpace = true
	headings, err := csvReader.Read()
	if err != nil {
		log.Printf("failed reading CSV data: %v", err)
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
	samplesChan <- InputData{
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
			samplesChan <- InputData{
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
