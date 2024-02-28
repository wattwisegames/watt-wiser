package main

import (
	"image"

	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"
	"git.sr.ht/~gioverse/skel/stream"
	"git.sr.ht/~whereswaldon/watt-wiser/backend"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

const (
	tabMonitor   = "monitor"
	tabBenchmark = "benchmark"
)

// UI is responsible for holding the state of and drawing the top-level UI.
type UI struct {
	ws   backend.WindowState
	expl *explorer.Explorer
	ds   Dataset

	chart       *ChartData
	benchmark   *Benchmark
	tab         widget.Enum
	launchBtn   widget.Clickable
	explorerBtn widget.Clickable
	launching   bool
	sensorsErr  string

	th           *material.Theme
	statusStream *stream.Stream[backend.Status]
	status       backend.Status
}

func NewUI(ws backend.WindowState, expl *explorer.Explorer) *UI {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	ui := &UI{
		ws:           ws,
		th:           th,
		expl:         expl,
		tab:          widget.Enum{Value: tabMonitor},
		statusStream: stream.New(ws.Controller, ws.Bundle.Datasource.Status),
	}
	ui.chart = NewChart(&ui.ds)
	ui.benchmark = NewBenchmark(ws, expl, &ui.ds)
	return ui
}

type (
	// UIRequest represents a request made by the UI to interact with a non-UI resource.
	UIRequest interface {
		isUIRequest()
	}
)

// Insert adds a datapoint to the UI's visualization.
func (ui *UI) Insert(sample backend.InputData) {
	switch sample.Kind {
	case backend.KindHeadings:
		ui.ds.Headings = sample.Headings
	case backend.KindSample:
		ui.ds.Insert(sample.Sample)
	}
}

// Update the state of the UI and generate events. Must be called until the second parameter
// (indicating the presence/absence of an event) returns false each frame.
func (ui *UI) Update(gtx C) {
	ui.statusStream.ReadInto(gtx, &ui.status, backend.Status{})
	switch ui.status.Mode {
	case backend.ModeReplaying:
		ui.chart.Update(gtx)
	case backend.ModeSensing:
		ui.chart.Update(gtx)
		ui.benchmark.Update(gtx)
	}
	if !ui.launching && ui.launchBtn.Clicked(gtx) {
		ui.launching = true
		ui.ws.Bundle.Datasource.LaunchSensors()
	}
	if ui.explorerBtn.Clicked(gtx) {
		ui.ws.Bundle.Datasource.LoadFromFile(ui.expl)
	}
}

// Layout the UI into the provided context.
func (ui *UI) Layout(gtx C) D {
	ui.Update(gtx)
	if ui.ds.Initialized() {
		if ui.tab.Value == tabMonitor {
			return ui.chart.Layout(gtx, ui.th)
		} else {
			return ui.benchmark.Layout(gtx, ui.th)
		}
	}
	l := material.Body1(ui.th, "No data yet.")
	return layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Middle,
		Spacing:   layout.SpaceAround,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			if ui.launching {
				gtx = gtx.Disabled()
			}
			return material.Button(ui.th, &ui.launchBtn, "Launch Sensors").Layout(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return material.Button(ui.th, &ui.explorerBtn, "Open Existing Trace").Layout(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return material.Body2(ui.th, ui.sensorsErr).Layout(gtx)
		}),
	)
}
