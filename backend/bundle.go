package backend

import (
	"context"
	"fmt"

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
	Benchmark  *Benchmark
	Datasource *Datasource
}

func NewBundle(appCtx context.Context, mutator *stream.Mutator) (Bundle, error) {
	ds, err := NewDatasource(appCtx, mutator)
	if err != nil {
		return Bundle{}, fmt.Errorf("failed constructing bundle: %w", err)
	}
	return Bundle{
		Benchmark:  NewBenchmark(mutator),
		Datasource: ds,
	}, nil
}
