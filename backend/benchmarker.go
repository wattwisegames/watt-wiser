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
}

func NewBenchmark(mutator *stream.Mutator) *Benchmark {
	return &Benchmark{
		pool: stream.NewMutationPool[string, BenchmarkData](mutator),
	}
}

type BenchmarkData struct {
	Command                                                              string
	Notes                                                                string
	PreBaselineStart, PreBaselineEnd, PostBaselineStart, PostBaselineEnd time.Time
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
			currentData := BenchmarkData{
				Command:          commandName,
				Notes:            notes,
				PreBaselineStart: time.Now(),
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
				currentData.PreBaselineEnd = t
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
			currentData.PostBaselineStart = time.Now()
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
				currentData.PostBaselineEnd = t
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
