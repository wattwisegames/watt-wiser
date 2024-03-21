package backend

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"gioui.org/x/explorer"
	"git.sr.ht/~gioverse/skel/stream"
)

type Benchmark struct {
	loadPool    *stream.MutationPool[struct{}, []BenchmarkData]
	executePool *stream.MutationPool[string, BenchmarkData]
	ds          *Datasource
}

func NewBenchmark(mutator *stream.Mutator, ds *Datasource) *Benchmark {
	return &Benchmark{
		executePool: stream.NewMutationPool[string, BenchmarkData](mutator),
		loadPool:    stream.NewMutationPool[struct{}, []BenchmarkData](mutator),
		ds:          ds,
	}
}

type ResultSet struct {
	Stats           []float64
	Series          []string
	StatsRows       int
	StatsCols       int
	SummaryJoules   []float64
	SummaryWatts    []float64
	SummaryDuration time.Duration
}

type BenchmarkData struct {
	SessionID                                                            string
	BenchmarkID                                                          string
	Command                                                              string
	Notes                                                                string
	PreBaselineStart, PreBaselineEnd, PostBaselineStart, PostBaselineEnd int64
	Err                                                                  error
	Results                                                              ResultSet `json:"-"`
}

func (b *BenchmarkData) attemptComputeResults(session Session) bool {
	series := session.Data.Headings()

	sectionsCount := 4
	rows := len(series) * sectionsCount
	baselines := make([]float64, len(series))
	cols := 4 // energy, minW, maxW, meanW for each baseline and runtime
	values := make([]float64, rows*cols)
	sectionStride := len(series) * cols
	finalSectionOffset := (sectionsCount - 1) * sectionStride
	var runDuration float64
	for section := 0; section < sectionsCount-1; section++ {
		var start, end int64
		isBaseline := false
		switch section {
		case 0:
			start = b.PreBaselineStart
			end = b.PreBaselineEnd
			isBaseline = true
		case 1:
			start = b.PreBaselineEnd
			end = b.PostBaselineStart
			runDuration = float64(end-start) / 1_000_000_000
		case 2:
			start = b.PostBaselineStart
			end = b.PostBaselineEnd
			isBaseline = true
		}
		sectionOffset := section * sectionStride
		for i, s := range session.Data {
			max, mean, min, sum, ok := s.RatesBetween(start, end)
			if !ok {
				// Need to retry once new data is available.
				return false
			}
			values[sectionOffset+i*cols+0] = sum
			values[sectionOffset+i*cols+1] = min
			values[sectionOffset+i*cols+2] = max
			values[sectionOffset+i*cols+3] = mean
			if isBaseline {
				baselines[i] += mean * .5
			} else {
				values[finalSectionOffset+i*cols+0] = sum
				values[finalSectionOffset+i*cols+1] = min
				values[finalSectionOffset+i*cols+2] = max
				values[finalSectionOffset+i*cols+3] = mean
			}
		}
	}
	rs := ResultSet{
		StatsRows:       rows,
		StatsCols:       cols,
		Series:          series,
		Stats:           values,
		SummaryDuration: time.Duration(b.PostBaselineStart-b.PreBaselineEnd) * time.Nanosecond,
	}
	for i, baseline := range baselines {
		values[finalSectionOffset+i*cols+0] -= baseline * float64(runDuration)
		rs.SummaryJoules = append(rs.SummaryJoules, values[finalSectionOffset+i*cols+0])
		values[finalSectionOffset+i*cols+1] -= baseline
		values[finalSectionOffset+i*cols+2] -= baseline
		values[finalSectionOffset+i*cols+3] -= baseline
		rs.SummaryWatts = append(rs.SummaryWatts, values[finalSectionOffset+i*cols+3])
	}
	b.Results = rs
	return true
}

// computeResults needs some session data to work from (in case the sessionStream channel is drained),
// but uses the sessionStream channel to determine when to re-attempt becuase new data has arrived.
func (b *BenchmarkData) computeResults(latestSession Session, sessionStream <-chan Session) {
	if b.attemptComputeResults(latestSession) {
		return
	}
	for session := range sessionStream {
		if b.attemptComputeResults(session) {
			return
		}
	}
}

func (b *Benchmark) StreamDatasetForBenchmarks(ctx context.Context, benchmarks ...BenchmarkData) <-chan Dataset {
	out := make(chan Dataset)
	go func() {
		defer close(out)
		sessionsToBenchmarks := map[string][]BenchmarkData{}
		slices.SortStableFunc(benchmarks, func(a, b BenchmarkData) int {
			return int(a.PreBaselineStart - b.PreBaselineStart)
		})
		for _, b := range benchmarks {
			sessionsToBenchmarks[b.SessionID] = append(sessionsToBenchmarks[b.SessionID], b)
		}
		ds := Dataset{}
		for sessionID, benchmarks := range sessionsToBenchmarks {
			subCtx, subCancel := context.WithCancel(ctx)
			session := <-b.ds.StreamSession(subCtx, sessionID)
			subCancel()
			for _, benchmark := range benchmarks {
				for _, series := range session.Data {
					ds = append(ds, NewBenchmarkSeriesFrom(series, benchmark))
				}
			}
		}
		out <- ds
		<-ctx.Done()
	}()
	return out
}

func randomIDString() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return strings.ReplaceAll(base64.StdEncoding.EncodeToString(buf[:]), "=", "")
}

func (b *Benchmark) Run(commandName, notes string, baselineDur time.Duration) (mutation *stream.Mutation[BenchmarkData], isNew bool) {
	return stream.Mutate(b.executePool, commandName, func(ctx context.Context) (values <-chan BenchmarkData) {
		out := make(chan BenchmarkData)
		go func() {
			defer close(out)
			cmd := exec.CommandContext(ctx, commandName)
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stdout
			startTime := time.Now()
			session := b.ds.SensingSession(ctx)
			currentData := BenchmarkData{
				SessionID:        session.ID,
				BenchmarkID:      randomIDString(),
				Command:          commandName,
				Notes:            notes,
				PreBaselineStart: startTime.UnixNano(),
			}
			timer := time.NewTimer(baselineDur)
			// Emit pre start time data.
			select {
			case out <- currentData:
			case <-ctx.Done():
				return
			}
			// Wait for timer to expire.
			select {
			case t := <-timer.C:
				// By adding the monotonic interval between now and the start time, we avoid clock skew.
				currentData.PreBaselineEnd = startTime.UnixNano() + t.Sub(startTime).Nanoseconds()
			case <-ctx.Done():
				return
			}
			// Emit pre end time data.
			select {
			case out <- currentData:
			case <-ctx.Done():
				return
			}
			err := cmd.Start()
			currentData.Err = err
			if err != nil {
				// Emit start error.
				select {
				case out <- currentData:
				case <-ctx.Done():
					return
				}
				// We've failed to run the command, so there's no pointer continuing.
				return
			}
			currentData.Err = cmd.Wait()
			// By adding the monotonic interval between now and the start time, we avoid clock skew.
			currentData.PostBaselineStart = startTime.UnixNano() + time.Since(startTime).Nanoseconds()
			timer.Reset(baselineDur)
			// Emit post start time data.
			select {
			case out <- currentData:
			case <-ctx.Done():
				return
			}
			// Wait for timer to expire.
			select {
			case t := <-timer.C:
				currentData.PostBaselineEnd = startTime.UnixNano() + t.Sub(startTime).Nanoseconds()
			case <-ctx.Done():
				return
			}
			// Calculate results.
			subCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			currentData.computeResults(session, b.ds.SensingSessionStream(subCtx))
			cancel()
			// Emit post end time data.
			select {
			case out <- currentData:
			case <-ctx.Done():
				return
			}
			// We're done.
			benchFile := benchmarkFileFor(session.ID)
			benchmarkData, err := os.ReadFile(benchFile)
			priorBenchmarks := []BenchmarkData{}
			isNewFile := false
			if errors.Is(err, fs.ErrNotExist) {
				isNewFile = true
			} else if err != nil {
				log.Printf("failed opening benchmark file %q: %v", benchFile, err)
			} else {
				if err := json.Unmarshal(benchmarkData, &priorBenchmarks); err != nil {
					log.Printf("failed reading benchmark file %q: %v", benchFile, err)
					newName := benchFile + ".corrupt"
					log.Printf("renaming corrupt benchmark file %q: %q", benchFile, newName)
					if err := os.Rename(benchFile, newName); err != nil {
						log.Printf("failed renaming corrupt benchmark file %q: %v", benchFile, err)
					}
				}
			}
			newName := benchFile + ".old"
			priorBenchmarks = append(priorBenchmarks, currentData)
			if !isNewFile {
				if err := os.Rename(benchFile, newName); err != nil && !errors.Is(err, fs.ErrNotExist) {
					log.Printf("failed renaming old benchmark file %q: %v", benchFile, err)
					log.Printf("not writing new benchmark to avoid overwriting previous data")
					return
				}
			}
			newJSON, err := json.MarshalIndent(priorBenchmarks, "", "  ")
			if err != nil {
				log.Printf("failed marshalling new benchmark data: %v", err)
				return
			}
			if err := os.WriteFile(benchFile, newJSON, 0o644); err != nil {
				log.Printf("failed writing new benchmark data: %v", err)
				return
			}
			if !isNewFile {
				if err := os.Remove(newName); err != nil {
					log.Printf("failed removing old benchmark file %q: %v", newName, err)
					return
				}
			}
		}()
		return out
	})
}

func (b *Benchmark) LoadBenchmarks(expl *explorer.Explorer) *stream.Mutation[[]BenchmarkData] {
	m, _ := stream.Mutate(b.loadPool, struct{}{}, func(ctx context.Context) (values <-chan []BenchmarkData) {
		out := make(chan []BenchmarkData)
		go func() {
			defer close(out)
			file, err := expl.ChooseFile()
			if err != nil {
				log.Printf("failed reading file: %v", err)
				return
			}
			defer file.Close()
			output := []BenchmarkData{}
			err = json.NewDecoder(file).Decode(&output)
			if err != nil && !errors.Is(err, io.EOF) {
				log.Printf("failed decoding json: %v", err)
				return
			}

			sessions := map[string][]BenchmarkData{}
			for _, bd := range output {
				sessions[bd.SessionID] = append(sessions[bd.SessionID], bd)
			}
			finalOutputs := []BenchmarkData{}
			for sessionID, relevantBenchmarks := range sessions {
				filename := sessionFileFor(sessionID)
				sessionFile, err := os.Open(filename)
				if err != nil {
					log.Printf("failed opening session file %q: %v", filename, err)
					continue
				}
				b.ds.LoadFromStreamWithID(sessionID, ModeReplaying, sessionFile)
				subCtx, subCancel := context.WithCancel(ctx)
				defer subCancel()
				sessionStream := b.ds.StreamSession(subCtx, sessionID)
				initialSession := <-sessionStream
				for _, benchmark := range relevantBenchmarks {
					benchmark.computeResults(initialSession, sessionStream)
					finalOutputs = append(finalOutputs, benchmark)
				}
				subCancel()
			}
			out <- finalOutputs
		}()
		return out
	})
	return m
}
