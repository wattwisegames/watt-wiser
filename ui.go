package main

import (
	"context"
	"image"
	"image/color"
	"log"

	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
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
	ws            backend.WindowState
	expl          *explorer.Explorer
	sessionStream *stream.Stream[backend.Session]
	session       backend.Session

	chart       *ChartData
	benchmark   *Benchmark
	tab         widget.Enum
	launchBtn   widget.Clickable
	explorerBtn widget.Clickable
	launching   bool
	sensorsErr  string

	th *material.Theme
}

func NewUI(ws backend.WindowState, expl *explorer.Explorer, sessionID string) *UI {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	ui := &UI{
		ws:   ws,
		th:   th,
		expl: expl,
		tab:  widget.Enum{Value: tabMonitor},
	}
	if sessionID != "" {
		ui.sessionStream = stream.New(ws.Controller, func(ctx context.Context) <-chan backend.Session {
			return ws.Bundle.Datasource.StreamSession(ctx, sessionID)
		})
	}
	ui.chart = NewChart()
	ui.benchmark = NewBenchmark(ws, expl)
	return ui
}

type (
	// UIRequest represents a request made by the UI to interact with a non-UI resource.
	UIRequest interface {
		isUIRequest()
	}
)

// Update the state of the UI and generate events. Must be called until the second parameter
// (indicating the presence/absence of an event) returns false each frame.
func (ui *UI) Update(gtx C) {
	if session, isNew := ui.sessionStream.ReadNew(gtx); isNew {
		ui.session = session
		ui.chart.SetDataset(session.Data)
	}
	ui.tab.Update(gtx)
	if ui.session.Err != nil {
		ui.sensorsErr = ui.session.Err.Error()
	}
	if !ui.launching && ui.launchBtn.Clicked(gtx) {
		ui.launching = true
		id, err := ui.ws.Bundle.Datasource.LaunchSensors()
		if err != nil {
			log.Printf("failed launching sensors: %w", err)
		} else {
			ui.sessionStream = stream.New(ui.ws.Controller, func(ctx context.Context) <-chan backend.Session {
				return ui.ws.Bundle.Datasource.StreamSession(ctx, id)
			})
		}
	}
	if ui.explorerBtn.Clicked(gtx) {
		_, mut, err := ui.ws.Bundle.Datasource.LoadFromFile(ui.expl)
		if err != nil {
			log.Printf("failed loading data from file: %v", err)
		} else {
			ui.sessionStream = stream.New(ui.ws.Controller, mut.Stream)
		}
	}
}

type TabStyle struct {
	state  *widget.Enum
	label  material.LabelStyle
	border widget.Border
	inset  layout.Inset
	value  string
	fill   color.NRGBA
}

func Tab(th *material.Theme, state *widget.Enum, value, display string) TabStyle {
	selected := state.Value == value
	ts := TabStyle{
		state: state,
		label: material.Body1(th, display),
		inset: layout.UniformInset(2),
		border: widget.Border{
			Width: 2,
			Color: th.ContrastBg,
		},
		value: value,
	}
	ts.label.Alignment = text.Middle
	if selected {
		ts.label.Color = th.ContrastFg
		ts.fill = th.ContrastBg
	}
	return ts
}

func (t TabStyle) Layout(gtx C) D {
	return t.inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return t.border.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return t.inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return t.state.Layout(gtx, t.value, func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						paint.FillShape(gtx.Ops, t.fill, clip.Rect{Max: gtx.Constraints.Min}.Op())
						return D{Size: gtx.Constraints.Min}
					}, t.label.Layout)
				})
			})
		})
	})
}

func (ui *UI) layoutMainArea(gtx C) D {
	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, Tab(ui.th, &ui.tab, tabMonitor, "Monitor").Layout),
				layout.Flexed(1, Tab(ui.th, &ui.tab, tabBenchmark, "Benchmark").Layout),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(ui.sensorsErr) == 0 {
				return D{}
			}
			l := material.Body1(ui.th, ui.sensorsErr)
			l.Color = color.NRGBA{R: 150, A: 255}
			return l.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if ui.tab.Value == tabMonitor {
				if ui.session.ID == "" {
					return ui.layoutStartScreen(gtx)
				}
				return ui.chart.Layout(gtx, ui.th)
			} else {
				return ui.benchmark.Layout(gtx, ui.th, ui.session.Data)
			}
		}),
	)
}

func (ui *UI) layoutStartScreen(gtx C) D {
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

// Layout the UI into the provided context.
func (ui *UI) Layout(gtx C) D {
	ui.Update(gtx)
	return ui.layoutMainArea(gtx)
}
