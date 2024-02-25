package main

import (
	"log"
	"os"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
	"gioui.org/x/explorer"
	"git.sr.ht/~gioverse/skel/stream"
	"git.sr.ht/~whereswaldon/watt-wiser/backend"
)

type benchmarkStatus uint8

const (
	statusNotStarted benchmarkStatus = iota
	statusRunningPreBaseline
	statusRunningCommand
	statusError
	statusRunningPostBaseline
	statusDone
)

func (b benchmarkStatus) String() string {
	switch b {
	case statusNotStarted:
		return "not started"
	case statusRunningPreBaseline:
		return "running pre baseline"
	case statusRunningCommand:
		return "running command"
	case statusError:
		return "error running command"
	case statusRunningPostBaseline:
		return "running post baseline"
	case statusDone:
		return "done"
	default:
		return "unknown"
	}
}

type Benchmark struct {
	commandEditor widget.Editor
	chooseFileBtn widget.Clickable
	startBtn      widget.Clickable
	commandName   string
	ws            backend.WindowState

	benchmarkStream *stream.Stream[backend.BenchmarkData]
	bd              backend.BenchmarkData
	status          benchmarkStatus
	explorer        *explorer.Explorer
	table           component.GridState
}

func NewBenchmark(ws backend.WindowState, expl *explorer.Explorer) *Benchmark {
	return &Benchmark{
		ws:       ws,
		explorer: expl,
	}
}

func (b *Benchmark) Update(gtx C) {
	for {
		ev, ok := b.commandEditor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			b.commandName = b.commandEditor.Text()
		}
	}
	if b.startBtn.Clicked(gtx) {
		b.runCommand(b.commandName)
	}
	if b.chooseFileBtn.Clicked(gtx) {
		f, err := b.explorer.ChooseFile()
		if err != nil {
			log.Printf("failed browsing for file: %v", err)
		} else {
			if osFile, ok := f.(*os.File); !ok {
				log.Printf("selected file of unexpected type: %T", f)
			} else {
				b.commandEditor.SetText(osFile.Name())
			}
		}
	}
	data, isNew := b.benchmarkStream.ReadNew(gtx)
	if isNew {
		b.bd = data
		switch {
		case b.bd.PostBaselineEnd != (time.Time{}):
			b.status = statusDone
		case b.bd.PostBaselineStart != (time.Time{}):
			b.status = statusRunningPostBaseline
		case b.bd.PreBaselineEnd != (time.Time{}):
			b.status = statusRunningCommand
		case b.bd.PreBaselineStart != (time.Time{}):
			b.status = statusRunningPreBaseline
		}
		if b.bd.Err != nil {
			b.status = statusError
		}
	}

}

func (b *Benchmark) runCommand(cmd string) {
	mut, ok := b.ws.Benchmark.Run(cmd, time.Second*2)
	if !ok {
		log.Printf("did not create new benchmarkStream")
		return
	}
	b.benchmarkStream = stream.New(b.ws.Controller, mut.Stream)
}

func (b *Benchmark) Layout(gtx C, th *material.Theme) D {
	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, material.Editor(th, &b.commandEditor, "command").Layout),
				layout.Rigid(material.Button(th, &b.chooseFileBtn, "Browse").Layout),
			)
		}),
		layout.Rigid(material.Button(th, &b.startBtn, "Start").Layout),
		layout.Rigid(func(gtx C) D {
			l := material.Body1(th, b.status.String())
			if b.bd.Err != nil {
				l.Text += " " + b.bd.Err.Error()
			}
			return l.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			tbl := component.Table(th, &b.table)
			const (
				rows = 3
				cols = 2
			)
			return tbl.Layout(gtx, rows, cols, func(axis layout.Axis, index, constraint int) int {
				if axis == layout.Vertical {
					return min(gtx.Dp(20), constraint)
				}
				return constraint / rows
			},
				func(gtx layout.Context, index int) layout.Dimensions {
					switch index {
					case 0:
						return material.Body1(th, "Phase Name").Layout(gtx)
					case 1:
						return material.Body1(th, "Watts").Layout(gtx)
					}
					return D{}
				},
				func(gtx layout.Context, row, col int) layout.Dimensions {
					switch col {
					case 0:
						switch row {
						case 0:
							return material.Body1(th, "Pre Baseline").Layout(gtx)
						case 1:
							return material.Body1(th, "Post Baseline").Layout(gtx)
						case 2:
							return material.Body1(th, "Application Execution").Layout(gtx)
						}
					case 1:
						switch row {
						case 0:
							return material.Body1(th, "Pre Baseline").Layout(gtx)
						case 1:
							return material.Body1(th, "Post Baseline").Layout(gtx)
						case 2:
							return material.Body1(th, "Application Execution").Layout(gtx)
						}
					}
					return D{}
				},
			)
		}),
	)
}
