package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"slices"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
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

type ResultSet struct {
	bd        backend.BenchmarkData
	stats     []float64
	series    []string
	statsRows int
	statsCols int
}

type Benchmark struct {
	commandEditor component.TextField
	notesEditor   component.TextField
	chooseFileBtn widget.Clickable
	disableStart  bool
	startBtn      widget.Clickable
	ws            backend.WindowState
	ds            *Dataset
	needResults   bool
	results       []ResultSet
	resultList    widget.List

	backendStatusStream *stream.Stream[backend.Status]
	backendStatus       backend.Status
	benchmarkStream     *stream.Stream[backend.BenchmarkData]
	bd                  backend.BenchmarkData
	status              benchmarkStatus
	explorer            *explorer.Explorer
	table               component.GridState
}

func NewBenchmark(ws backend.WindowState, expl *explorer.Explorer, ds *Dataset) *Benchmark {
	return &Benchmark{
		ws:                  ws,
		explorer:            expl,
		ds:                  ds,
		resultList:          widget.List{List: layout.List{Axis: layout.Vertical}},
		backendStatusStream: stream.New(ws.Controller, ws.Bundle.Datasource.Status),
	}
}

func (b *Benchmark) Update(gtx C, th *material.Theme) {
	b.commandEditor.Update(gtx, th, "Executable to Benchmark")
	b.notesEditor.Update(gtx, th, "Benchmark Notes")
	if b.startBtn.Clicked(gtx) {
		b.disableStart = true
		b.runCommand(b.commandEditor.Text(), b.notesEditor.Text())
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
	b.backendStatusStream.ReadInto(gtx, &b.backendStatus, backend.Status{
		Mode: backend.ModeNone,
	})
	data, isNew := b.benchmarkStream.ReadNew(gtx)
	if isNew {
		b.bd = data
		switch {
		case b.bd.PostBaselineEnd != 0:
			b.status = statusDone
			b.disableStart = false
			b.needResults = true
		case b.bd.PostBaselineStart != 0:
			b.status = statusRunningPostBaseline
		case b.bd.PreBaselineEnd != 0:
			b.status = statusRunningCommand
		case b.bd.PreBaselineStart != 0:
			b.status = statusRunningPreBaseline
		}
		if b.bd.Err != nil {
			b.status = statusError
		}
	}
	b.computeResults()

}

func (b *Benchmark) runCommand(cmd, notes string) {
	mut, ok := b.ws.Benchmark.Run(cmd, notes, time.Second*2)
	if !ok {
		log.Printf("did not create new benchmarkStream")
		return
	}
	b.benchmarkStream = stream.New(b.ws.Controller, mut.Stream)
}

func (b *Benchmark) computeResults() {
	if !b.needResults {
		return
	}
	series := slices.Clone(b.ds.Headings)
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
			start = b.bd.PreBaselineStart
			end = b.bd.PreBaselineEnd
			isBaseline = true
		case 1:
			start = b.bd.PreBaselineEnd
			end = b.bd.PostBaselineStart
			runDuration = float64(end-start) / 1_000_000_000
		case 2:
			start = b.bd.PostBaselineStart
			end = b.bd.PostBaselineEnd
			isBaseline = true
		}
		sectionOffset := section * sectionStride
		for i, s := range b.ds.Series {
			max, mean, min, sum, ok := s.RatesBetween(start, end)
			if !ok {
				// Need to retry once new data is available.
				return
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
	for i, baseline := range baselines {
		values[finalSectionOffset+i*cols+0] -= baseline * float64(runDuration)
		values[finalSectionOffset+i*cols+1] -= baseline
		values[finalSectionOffset+i*cols+2] -= baseline
		values[finalSectionOffset+i*cols+3] -= baseline
	}

	b.results = append(b.results, ResultSet{
		statsRows: rows,
		statsCols: cols,
		series:    series,
		stats:     values,
		bd:        b.bd,
	})
	b.needResults = false
}

func (b *Benchmark) Layout(gtx C, th *material.Theme) D {
	inset := layout.UniformInset(2)
	b.Update(gtx, th)
	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{
				Alignment: layout.Baseline,
			}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return b.commandEditor.Layout(gtx, th, "Executable to benchmark")
					})
				}),
				layout.Rigid(func(gtx C) D {
					return inset.Layout(gtx, material.Button(th, &b.chooseFileBtn, "Browse").Layout)
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return b.notesEditor.Layout(gtx, th, "Benchmark Notes")
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{
				Alignment: layout.Baseline,
			}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, &b.startBtn, "Start")
						if b.disableStart || b.commandEditor.Len() == 0 {
							gtx = gtx.Disabled()
						}
						return btn.Layout(gtx)
					})
				}),
				layout.Flexed(1, func(gtx C) D {
					return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Body1(th, "Status: "+b.status.String())
						if b.bd.Err != nil {
							l.Text += " " + b.bd.Err.Error()
						}
						return l.Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.List(th, &b.resultList).Layout(gtx, len(b.results), func(gtx layout.Context, index int) layout.Dimensions {
				results := b.results[index]

				gtx.Constraints.Min.Y = 0
				tbl := component.Table(th, &b.table)
				prefixCols := 2
				longest := material.Body1(th, "Post Baseline")
				origConstraints := gtx.Constraints
				gtx.Constraints.Min = image.Point{}
				longestDims, _ := rec(gtx, func(gtx C) D {
					return layout.UniformInset(2).Layout(gtx, longest.Layout)
				})
				flexedColumns := 1
				rigidColumns := (results.statsCols + prefixCols) - flexedColumns
				gtx.Constraints = origConstraints
				return tbl.Layout(gtx, results.statsRows, results.statsCols+prefixCols, func(axis layout.Axis, index, constraint int) int {
					if axis == layout.Vertical {
						return min(longestDims.Size.Y, constraint)
					}
					if index == 1 {
						return (constraint - (longestDims.Size.X * rigidColumns)) / flexedColumns
					}
					return longestDims.Size.X
				},
					func(gtx layout.Context, index int) layout.Dimensions {
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								paint.FillShape(gtx.Ops, th.ContrastBg, clip.Rect{Max: gtx.Constraints.Min}.Op())
								return D{Size: gtx.Constraints.Min}
							},
							func(gtx layout.Context) layout.Dimensions {
								l := material.Body1(th, "")
								l.MaxLines = 1
								l.Color = th.ContrastFg
								if index == 0 {
									l.Text = "Phase"
								} else if index == 1 {
									l.Text = "Sensor Name"
								} else {
									switch index - prefixCols {
									case 0:
										l.Text = "sum(J)"
									case 1:
										l.Text = "min(W)"
									case 2:
										l.Text = "max(W)"
									case 3:
										l.Text = "avg(W)"
									}
									l.Alignment = text.End
								}
								return l.Layout(gtx)
							},
						)
					},
					func(gtx layout.Context, row, col int) layout.Dimensions {
						phase := row / len(b.ds.Series)
						return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							c := color.NRGBA{R: 100, G: 100, B: 100, A: 0}
							switch phase {
							case 1:
								c.A = 100
								c.G += 50
								c.R -= 50
								c.B -= 50
							case 3:
								c.A = 100
								c.G += 50
								c.R -= 50
								c.B -= 50
							}
							if row&1 == 0 {
								c.A += 50
							}
							paint.FillShape(gtx.Ops, c, clip.Rect{Max: gtx.Constraints.Min}.Op())
							return D{Size: gtx.Constraints.Min}
						},
							func(gtx layout.Context) layout.Dimensions {
								if col == 0 {
									var label string
									switch phase {
									case 0:
										label = "Pre Baseline"
									case 1:
										label = "Benchmark"
									case 2:
										label = "Post Baseline"
									case 3:
										label = "Adjusted"
									}
									l := material.Body1(th, label)
									l.MaxLines = 1
									return l.Layout(gtx)
								} else if col == 1 {
									l := material.Body1(th, b.ds.Headings[row%len(b.ds.Series)])
									l.MaxLines = 1
									return l.Layout(gtx)
								}
								col -= prefixCols
								val := results.stats[row*results.statsCols+col]
								l := material.Body1(th, fmt.Sprintf("%0.2f", val))
								l.Alignment = text.End
								l.MaxLines = 1
								return l.Layout(gtx)
							},
						)
					},
				)
			})
		}),
	)
}
