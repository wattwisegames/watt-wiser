package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
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
	disableStart  bool
	startBtn      widget.Clickable
	commandName   string
	ws            backend.WindowState
	ds            *Dataset

	benchmarkStream *stream.Stream[backend.BenchmarkData]
	bd              backend.BenchmarkData
	status          benchmarkStatus
	explorer        *explorer.Explorer
	table           component.GridState
}

func NewBenchmark(ws backend.WindowState, expl *explorer.Explorer, ds *Dataset) *Benchmark {
	return &Benchmark{
		ws:       ws,
		explorer: expl,
		ds:       ds,
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
		b.disableStart = true
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
			b.disableStart = false
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
			return layout.Flex{
				Alignment: layout.Baseline,
			}.Layout(gtx,
				layout.Flexed(1, material.Editor(th, &b.commandEditor, "command").Layout),
				layout.Rigid(material.Button(th, &b.chooseFileBtn, "Browse").Layout),
			)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{
				Alignment: layout.Baseline,
			}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					btn := material.Button(th, &b.startBtn, "Start")
					if b.disableStart || b.commandEditor.Len() == 0 {
						gtx = gtx.Disabled()
					}
					return btn.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx C) D {
					l := material.Body1(th, b.status.String())
					if b.bd.Err != nil {
						l.Text += " " + b.bd.Err.Error()
					}
					return l.Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = 0
			tbl := component.Table(th, &b.table)
			rows := len(b.ds.Series)
			cols := 1 + 4*3 // energy, minW, maxW, meanW for each baseline and runtime
			values := make([]float64, rows*cols)
			for i, s := range b.ds.Series {
				max, mean, min, _ := s.RatesBetween(b.bd.PreBaselineStart.UnixNano(), b.bd.PreBaselineEnd.UnixNano())
				values[i*cols+0] = 0
				values[i*cols+1] = min
				values[i*cols+2] = max
				values[i*cols+3] = mean
				max, mean, min, _ = s.RatesBetween(b.bd.PostBaselineStart.UnixNano(), b.bd.PostBaselineEnd.UnixNano())
				values[i*cols+4] = 0
				values[i*cols+5] = min
				values[i*cols+6] = max
				values[i*cols+7] = mean
				max, mean, min, _ = s.RatesBetween(b.bd.PreBaselineEnd.UnixNano(), b.bd.PostBaselineStart.UnixNano())
				values[i*cols+8] = 0
				values[i*cols+9] = min
				values[i*cols+10] = max
				values[i*cols+11] = mean
			}
			longest := material.Body1(th, "Post sum(W)")
			origConstraints := gtx.Constraints
			gtx.Constraints.Min = image.Point{}
			longestDims, _ := rec(gtx, func(gtx C) D {
				return layout.UniformInset(2).Layout(gtx, longest.Layout)
			})
			gtx.Constraints = origConstraints
			return tbl.Layout(gtx, rows, cols, func(axis layout.Axis, index, constraint int) int {
				if axis == layout.Vertical {
					return min(longestDims.Size.Y, constraint)
				}
				if index == 0 {
					return constraint / 3
				}
				return longestDims.Size.X
			},
				func(gtx layout.Context, index int) layout.Dimensions {
					if index == 0 {
						return material.Body1(th, "Sensor Name").Layout(gtx)
					}
					var label string
					switch mod := (index - 1) % 4; mod {
					case 0:
						label = "sum(J)"
					case 1:
						label = "min(W)"
					case 2:
						label = "max(W)"
					case 3:
						label = "avg(W)"
					}
					var phase string
					switch mod := (index - 1) / 4; mod {
					case 0:
						phase = "Pre"
					case 1:
						phase = "Post"
					case 2:
						phase = "Run"
					}
					l := material.Body1(th, phase+" "+label)
					l.MaxLines = 1
					l.Alignment = text.End
					return l.Layout(gtx)
				},
				func(gtx layout.Context, row, col int) layout.Dimensions {
					if col == 0 {
						l := material.Body1(th, b.ds.Headings[row])
						l.MaxLines = 1
						return l.Layout(gtx)
					}
					col -= 1
					val := values[row*cols+col]
					l := material.Body1(th, fmt.Sprintf("%0.2f", val))
					l.Alignment = text.End
					l.MaxLines = 1
					return l.Layout(gtx)
				},
			)
		}),
	)
}
