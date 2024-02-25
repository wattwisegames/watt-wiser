package main

import (
	"log"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
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
	startBtn      widget.Clickable
	commandName   string
	ws            backend.WindowState

	benchmarkStream *stream.Stream[backend.BenchmarkData]
	bd              backend.BenchmarkData
	status          benchmarkStatus
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
		Axis:    layout.Vertical,
		Spacing: layout.SpaceAround,
	}.Layout(gtx,
		layout.Rigid(material.Editor(th, &b.commandEditor, "command").Layout),
		layout.Rigid(material.Button(th, &b.startBtn, "Start").Layout),
		layout.Rigid(material.Body1(th, b.status.String()).Layout),
	)
}
