package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"time"

	"git.sr.ht/~gioverse/skel/stream"
)

type Benchmark struct {
	pool *stream.MutationPool[string, BenchmarkData]
	ds   *Datasource
}

func NewBenchmark(mutator *stream.Mutator, ds *Datasource) *Benchmark {
	return &Benchmark{
		pool: stream.NewMutationPool[string, BenchmarkData](mutator),
		ds:   ds,
	}
}

type BenchmarkData struct {
	Command                                                              string
	Notes                                                                string
	PreBaselineStart, PreBaselineEnd, PostBaselineStart, PostBaselineEnd int64
	Err                                                                  error
}

func (b *Benchmark) Run(commandName, notes string, baselineDur time.Duration) (mutation *stream.Mutation[BenchmarkData], isNew bool) {
	return stream.Mutate(b.pool, commandName, func(ctx context.Context) (values <-chan BenchmarkData) {
		out := make(chan BenchmarkData)
		go func() {
			defer close(out)
			cmd := exec.CommandContext(ctx, commandName)
			cmd.Stderr = os.Stderr
			cmd.Stdout = os.Stdout
			startTime := time.Now()
			currentData := BenchmarkData{
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
			// Emit post end time data.
			select {
			case out <- currentData:
			case <-ctx.Done():
				return
			}
			// We're done.
			session := b.ds.SensingSession(ctx)
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
