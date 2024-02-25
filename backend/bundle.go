package backend

import (
	"context"

	"gioui.org/app"
	"git.sr.ht/~gioverse/skel/stream"
)

type WindowState struct {
	Bundle
	Controller *stream.Controller
}

func NewWindowState(ctx context.Context, bundle Bundle, win *app.Window) WindowState {
	return WindowState{
		Bundle:     bundle,
		Controller: stream.NewController(ctx, win.Invalidate),
	}
}

type Bundle struct {
	Benchmark *Benchmark
}

func NewBundle(mutator *stream.Mutator) Bundle {
	return Bundle{
		Benchmark: NewBenchmark(mutator),
	}
}
