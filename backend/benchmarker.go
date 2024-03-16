package backend

import (
	"context"
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
		}()
		return out
	})
}
