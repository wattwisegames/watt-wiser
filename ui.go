package main

import (
	"image"

	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"
	"git.sr.ht/~whereswaldon/watt-wiser/backend"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

const (
	modeMonitor   = "monitor"
	modeBenchmark = "benchmark"
)

// UI is responsible for holding the state of and drawing the top-level UI.
type UI struct {
	mode        widget.Enum
	chart       ChartData
	benchmark   *Benchmark
	launchBtn   widget.Clickable
	explorerBtn widget.Clickable
	launching   bool
	sensorsErr  string

	th *material.Theme
}

func NewUI(ws backend.WindowState, expl *explorer.Explorer) *UI {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	return &UI{
		th: th,
		mode: widget.Enum{
			Value: modeBenchmark,
		},
		benchmark: NewBenchmark(ws, expl),
	}
}

type (
	// UIRequest represents a request made by the UI to interact with a non-UI resource.
	UIRequest interface {
		isUIRequest()
	}
	// LoadFileRequest indicates that the UI wants to load a trace file from a file picker.
	LoadFileRequest struct{}
	// LaunchSensorsRequest indicates that the UI wants to launch the sensors itself and
	// consume their data.
	LaunchSensorsRequest struct{}
)

func (LoadFileRequest) isUIRequest()      {}
func (LaunchSensorsRequest) isUIRequest() {}

// Insert adds a datapoint to the UI's visualization.
func (ui *UI) Insert(sample inputData) {
	switch sample.Kind {
	case kindHeadings:
		ui.chart.Headings = sample.Headings
	case kindSample:
		ui.chart.Insert(sample.Sample)
	}
}

// Update the state of the UI and generate events. Must be called until the second parameter
// (indicating the presence/absence of an event) returns false each frame.
func (ui *UI) Update(gtx C) (UIRequest, bool) {
	_ = ui.mode.Update(gtx)
	switch ui.mode.Value {
	case modeMonitor:
		ui.chart.Update(gtx)
	case modeBenchmark:
		ui.benchmark.Update(gtx)
	}
	if !ui.launching && ui.launchBtn.Clicked(gtx) {
		ui.launching = true
		return LaunchSensorsRequest{}, true
	}
	if ui.explorerBtn.Clicked(gtx) {
		return LoadFileRequest{}, true
	}
	return nil, false
}

// Layout the UI into the provided context.
func (ui *UI) Layout(gtx C) D {
	for {
		_, ok := ui.Update(gtx)
		if !ok {
			break
		}
	}
	if ui.chart.Initialized() {
		if ui.mode.Value == modeMonitor {
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
