package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"sort"
	"strconv"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

var pauseIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.AVPause)
	return icon
}()

var playIcon = func() *widget.Icon {
	icon, _ := widget.NewIcon(icons.AVPlayArrow)
	return icon
}()

type timeslice struct {
	tsStart, tsEnd int64
	xL, xR         float32
	y              float32
	mean           float64
}

type ChartData struct {
	*Dataset
	seriesSlices [][]timeslice
	Enabled      []*widget.Bool
	Stacked      widget.Bool
	zoom         gesture.Scroll
	pan          gesture.Scroll
	panBar       widget.Scrollbar
	xOffset      int64
	xOrigin      int64
	paused       bool
	pauseBtn     widget.Clickable
	keyTable     component.GridState
	nsPerDp      int64
	// returnPath is a scratch slice used to build each data series'
	// path.
	returnPath []f32.Point
	// hover gesture state
	pos       f32.Point
	isHovered bool
}

func NewChart(ds *Dataset) *ChartData {
	return &ChartData{
		Dataset: ds,
		nsPerDp: 10_000_000,
	}
}

var colors = []color.NRGBA{
	{R: 0xa4, G: 0x63, B: 0x3a, A: 0xff},
	{R: 0x85, G: 0x76, B: 0x25, A: 0xff}, //#857625
	{R: 0x51, G: 0x85, B: 0x4d, A: 0xff}, //#51854d
	{R: 0x2b, G: 0x7f, B: 0xa8, A: 0xff}, //#2b7fa8
	{R: 0x72, G: 0x6c, B: 0xae, A: 0xff}, //#726cae
	{R: 0x97, G: 0x5f, B: 0x91, A: 0xff}, //975f91
	{R: 0xff, A: 0xff},
	{G: 0xff, A: 0xff},
	{B: 0xff, A: 0xff},
	{R: 0xf0, G: 0xf0, A: 0xff},
}

func rec(gtx C, w layout.Widget) (D, op.CallOp) {
	macro := op.Record(gtx.Ops)
	dims := w(gtx)
	call := macro.Stop()
	return dims, call
}

func (c *ChartData) layoutYAxisLabels(gtx C, th *material.Theme, pxPerWatt int, minRange, maxRange float64) D {
	origConstraints := gtx.Constraints
	// Flip X and Y to draw our axis horizontally.
	gtx.Constraints.Max.X, gtx.Constraints.Max.Y = gtx.Constraints.Max.Y, gtx.Constraints.Max.X
	gtx.Constraints.Min = image.Point{}

	// Measure the (presumably) widest label.
	maxYLine := int(floor(maxRange))
	label := material.Body1(th, strconv.Itoa(maxYLine))
	maxLabelDims, maxLabelCall := rec(gtx, label.Layout)
	halfLabelWidth := maxLabelDims.Size.X / 2

	gap := gtx.Dp(10)

	axisMacro := op.Record(gtx.Ops)

	yAxisLabel := material.Body2(th, fmt.Sprintf("Power Draw in Watts (scale = %.1f Dp/Watt)", gtx.Metric.PxToDp(pxPerWatt)))
	yAxisLabel.MaxLines = 1
	axisDims := layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Middle,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return yAxisLabel.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// Lay out the minimum label.
			label.Text = strconv.Itoa(0)
			minDims := label.Layout(gtx)

			usedX := minDims.Size.X + gap
			maxX := gtx.Constraints.Max.X - maxLabelDims.Size.X

			for i := 1; i < maxYLine; i++ {
				if usedX+halfLabelWidth > pxPerWatt*i {
					continue
				} else if pxPerWatt*i+halfLabelWidth+gap > maxX {
					break
				}
				label.Text = strconv.Itoa(i)
				labelDims, labelCall := rec(gtx, label.Layout)
				stack := op.Offset(image.Point{
					X: pxPerWatt*i - labelDims.Size.X/2,
				}).Push(gtx.Ops)
				labelCall.Add(gtx.Ops)
				stack.Pop()
				usedX = pxPerWatt*i + labelDims.Size.X/2 + gap
			}
			stack := op.Offset(image.Point{
				X: gtx.Constraints.Max.X - maxLabelDims.Size.X,
			}).Push(gtx.Ops)
			maxLabelCall.Add(gtx.Ops)
			stack.Pop()
			return D{Size: image.Point{
				X: gtx.Constraints.Max.X,
				Y: maxLabelDims.Size.Y,
			}}
		}),
	)

	axisCall := axisMacro.Stop()

	halfAxisHeight := float32(axisDims.Size.Y) * .5

	defer op.Affine(
		f32.Affine2D{}.
			Rotate(f32.Pt(halfAxisHeight, halfAxisHeight), -math.Pi/2).
			Offset(f32.Point{Y: float32(gtx.Constraints.Max.X - axisDims.Size.Y)}),
	).Push(gtx.Ops).Pop()
	axisCall.Add(gtx.Ops)

	return D{Size: origConstraints.Max}
}

func (c *ChartData) Update(gtx C) {
	for len(c.Enabled) < len(c.Headings) {
		c.Enabled = append(c.Enabled, &widget.Bool{Value: true})
	}
	for len(c.seriesSlices) < len(c.Headings) {
		c.seriesSlices = append(c.seriesSlices, nil)
	}
	if c.pauseBtn.Clicked(gtx) {
		c.paused = !c.paused
		c.xOrigin = c.DomainMax + c.xOffset
		c.xOffset = 0
	}
	c.Stacked.Update(gtx)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: c,
			Kinds:  pointer.Enter | pointer.Leave | pointer.Move,
		})
		if !ok {
			break
		}
		switch ev := ev.(type) {
		case pointer.Event:
			switch ev.Kind {
			case pointer.Enter:
				c.isHovered = true
				c.pos = ev.Position
			case pointer.Leave, pointer.Cancel:
				c.isHovered = false
			case pointer.Move:
				c.pos = ev.Position
			}
		}
	}
}

func (c *ChartData) Layout(gtx C, th *material.Theme) D {
	c.Update(gtx)
	if len(c.Series) < 1 {
		return D{Size: gtx.Constraints.Max}
	}
	minRangeLabel := material.Body1(th, strconv.FormatFloat(0, 'f', 3, 64))
	origConstraints := gtx.Constraints
	gtx.Constraints.Min = image.Point{}

	// Determine the amount of space to reserve for axis labels.
	macro := op.Record(gtx.Ops)
	axisLabelDims := minRangeLabel.Layout(gtx)
	_ = macro.Stop()

	// Determine the space occupied by the key.
	macro = op.Record(gtx.Ops)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	keyDims := c.layoutControls(gtx, th)
	keyCall := macro.Stop()

	// Lay out the plot in the remaining space after accounting for axis
	// labels and the key.
	gtx.Constraints = origConstraints.SubMax(image.Point{
		X: axisLabelDims.Size.Y * 2,
		Y: axisLabelDims.Size.Y,
	}.Add(image.Pt(0, keyDims.Size.Y)))
	macro = op.Record(gtx.Ops)
	dims, domainMin, domainMax, pxPerWatt, rangeMin, rangeMax := c.layoutPlot(gtx, th)
	domainEndSecs := float64(domainMax-c.DomainMax) / 1_000_000_000
	domainIntervalSecs := float64(domainMax-domainMin) / 1_000_000_000
	domainStartSecs := domainEndSecs - domainIntervalSecs
	plotCall := macro.Stop()
	gtx.Constraints = origConstraints
	minDomainLabel := material.Body1(th, strconv.FormatFloat(domainStartSecs, 'f', 3, 64)+"s")
	maxDomainLabel := material.Body1(th, strconv.FormatFloat(domainEndSecs, 'f', 3, 64)+"s")
	xAxisLabel := material.Body2(th, fmt.Sprintf("Time (spans %.2f s, scale = %d ns/Dp)", domainIntervalSecs, c.nsPerDp))
	xAxisLabel.MaxLines = 1
	xAxisLabel.Alignment = text.Middle
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							gtx.Constraints.Min = image.Point{}
							gtx.Constraints.Max.X = axisLabelDims.Size.Y * 2
							return c.layoutYAxisLabels(gtx, th, pxPerWatt, rangeMin, rangeMax)
						}),
						layout.Rigid(func(gtx C) D {
							gtx.Constraints = layout.Exact(image.Point{
								X: axisLabelDims.Size.Y * 2,
								Y: axisLabelDims.Size.Y,
							})
							icon := pauseIcon
							if c.paused {
								icon = playIcon
							}
							return material.Clickable(gtx, &c.pauseBtn, func(gtx layout.Context) layout.Dimensions {
								return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return icon.Layout(gtx, th.Fg)
								})
							})
						}),
					)
				}),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							plotCall.Add(gtx.Ops)
							return dims
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Flex{
								Axis:      layout.Horizontal,
								Alignment: layout.Baseline,
							}.Layout(gtx,
								layout.Rigid(minDomainLabel.Layout),
								layout.Flexed(1, xAxisLabel.Layout),
								layout.Rigid(maxDomainLabel.Layout),
							)
						}),
					)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			keyCall.Add(gtx.Ops)
			return keyDims
		}),
	)
}

func (c *ChartData) layoutControls(gtx C, th *material.Theme) D {
	table := component.Table(th, &c.keyTable)
	table.HScrollbarStyle.Indicator.MinorWidth = 0
	table.HScrollbarStyle.Track.MinorPadding = 0
	table.VScrollbarStyle.Indicator.MinorWidth = 0
	table.VScrollbarStyle.Track.MinorPadding = 0
	colorColWidth := gtx.Dp(50)
	totalColWidth := gtx.Dp(100)
	nameColWidth := gtx.Constraints.Max.X - colorColWidth - 2*totalColWidth - gtx.Dp(table.VScrollbarStyle.Width())
	rowHeight := gtx.Sp(20)
	const (
		colorCol = iota
		seriesNameCol
		totalJoulesCol
		totalWattHoursCol
		numCols
	)
	return table.Layout(gtx, len(c.Headings)+1, numCols,
		func(axis layout.Axis, index, constraint int) int {
			if axis == layout.Vertical {
				return min(constraint, rowHeight)
			}

			var size int
			switch index {
			case colorCol:
				size = colorColWidth
			case seriesNameCol:
				size = nameColWidth
			case totalJoulesCol:
				size = totalColWidth
			case totalWattHoursCol:
				size = totalColWidth
			}
			return min(size, constraint)
		},
		func(gtx layout.Context, index int) layout.Dimensions {
			var l material.LabelStyle
			switch index {
			case colorCol:
				l = material.Body1(th, "Color")
			case seriesNameCol:
				l = material.Body1(th, "Data Series Name")
				l.Alignment = text.Middle
			case totalJoulesCol:
				l = material.Body1(th, "Total Joules")
				l.Alignment = text.End
			case totalWattHoursCol:
				l = material.Body1(th, "Total Wh")
				l.Alignment = text.End
			default:
				l = material.Body1(th, "???")
			}
			l.Color = th.ContrastFg
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					paint.FillShape(gtx.Ops, th.ContrastBg, clip.Rect{Max: gtx.Constraints.Max}.Op())
					return D{Size: gtx.Constraints.Min}
				}, func(gtx layout.Context) layout.Dimensions {
					return l.Layout(gtx)
				},
			)
		},
		func(gtx layout.Context, row, col int) (dims layout.Dimensions) {
			defer func() {
				dims.Size = gtx.Constraints.Constrain(dims.Size)
			}()
			dims = layout.UniformInset(2).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if row == len(c.Headings) {
					switch col {
					case colorCol:
						return layout.Dimensions{Size: gtx.Constraints.Min}
					case seriesNameCol:
						return material.Body2(th, "Total of enabled series").Layout(gtx)
					case totalJoulesCol:
						sum := 0.0
						for sumIdx, series := range c.Series {
							if c.Enabled[sumIdx].Value {
								sum += series.Sum
							}
						}
						l := material.Body2(th, fmt.Sprintf("%.2f", sum))
						l.Alignment = text.End
						return l.Layout(gtx)
					case totalWattHoursCol:
						sum := 0.0
						for sumIdx, series := range c.Series {
							if c.Enabled[sumIdx].Value {
								sum += series.Sum
							}
						}
						sum = sum / 3600
						l := material.Body2(th, fmt.Sprintf("%.4f", sum))
						l.Alignment = text.End
						return l.Layout(gtx)
					default:
						return material.Body2(th, "???").Layout(gtx)
					}
				}
				c.Enabled[row].Update(gtx)
				enabled := c.Enabled[row].Value
				disabledAlpha := uint8(100)
				switch col {
				case colorCol:
					return c.Enabled[row].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							sideLen := gtx.Dp(10)
							sz := image.Pt(sideLen, sideLen)
							fullColor := colors[row]
							if !enabled {
								fullColor.A = disabledAlpha
							}
							paint.FillShape(gtx.Ops, fullColor, clip.Rect{Max: sz}.Op())
							return D{Size: sz}
						})
					})
				case seriesNameCol:
					l := material.Body2(th, c.Headings[row])
					if !enabled {
						l.Color.A = disabledAlpha
					}
					return l.Layout(gtx)
				case totalJoulesCol:
					l := material.Body2(th, fmt.Sprintf("%.2f", c.Series[row].Sum))
					if !enabled {
						l.Color.A = disabledAlpha
					}
					l.Alignment = text.End
					return l.Layout(gtx)
				case totalWattHoursCol:
					l := material.Body2(th, fmt.Sprintf("%.4f", c.Series[row].Sum/3600))
					if !enabled {
						l.Color.A = disabledAlpha
					}
					l.Alignment = text.End
					return l.Layout(gtx)
				default:
					return D{Size: gtx.Constraints.Max}
				}
			})
			if row&1 != 0 {
				col := colors[row]
				col.A = 50
				paint.FillShape(gtx.Ops, col, clip.Rect{Max: gtx.Constraints.Max}.Op())
			}
			return dims
		})
}

func (c *ChartData) layoutPlot(gtx C, th *material.Theme) (dims D, domainMin, domainMax int64, pxPerWatt int, rangeMin, rangeMax float64) {
	dist := c.zoom.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, image.Rect(0, -1e6, 0, 1e6))
	if dist != 0 {
		proportion := 1 + float64(dist)/float64(gtx.Constraints.Max.Y)
		c.nsPerDp = max(int64(math.Round(float64(c.nsPerDp)*proportion)), 1)
	}
	var pannedNS int64
	dist = c.pan.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Horizontal, image.Rect(-1e6, 0, 1e6, 0))
	if dist != 0 {
		pannedNS += int64(gtx.Metric.PxToDp(dist) * unit.Dp(c.nsPerDp))
	}
	totalDomainInterval := c.DomainMax - c.DomainMin
	if panDist := c.panBar.ScrollDistance(); panDist != 0 {
		pannedNS += int64(panDist * float32(totalDomainInterval))
	}
	origin := c.DomainMax
	if c.paused {
		origin = c.xOrigin
	}
	if pannedNS != 0 {
		if endCandidate := origin + c.xOffset + pannedNS; endCandidate >= c.DomainMin && endCandidate <= c.DomainMax {
			c.xOffset += pannedNS
		}
	}
	maxVisibleX := origin + c.xOffset
	if maxVisibleX > c.DomainMax {
		maxVisibleX = c.DomainMax
	}
	numDp := gtx.Metric.PxToDp(gtx.Constraints.Max.X)
	visibleDomainInterval := int64(math.Round(float64(numDp * unit.Dp(c.nsPerDp))))
	// visibleDomainEnd forces the first datapoint to be an even multiple of the current
	// scale, which prevents weird cross-frame sampling artifacts.
	visibleDomainEnd := ((maxVisibleX) / c.nsPerDp) * c.nsPerDp
	visibleDomainStart := visibleDomainEnd - visibleDomainInterval
	var maxY int
	maxY, pxPerWatt, rangeMax = c.computeRange(gtx)
	c.computeVisible(gtx, maxY, visibleDomainStart, visibleDomainEnd, rangeMax)
	end := visibleDomainEnd - c.DomainMin
	start := visibleDomainStart - c.DomainMin
	vpStart := float32(start) / float32(totalDomainInterval)
	vpEnd := float32(end) / float32(totalDomainInterval)

	dims = c.Stacked.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.S}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				macro := op.Record(gtx.Ops)
				c.pan.Add(gtx.Ops)
				c.zoom.Add(gtx.Ops)
				event.Op(gtx.Ops, c)
				// Draw grid underneath plot.
				c.layoutYAxisGrid(gtx, maxY, pxPerWatt)
				if !c.Stacked.Value {
					c.layoutLinePlot(gtx, maxY, pxPerWatt, rangeMax)
				} else {
					c.layoutStackPlot(gtx, maxY, pxPerWatt, rangeMax)
				}
				call := macro.Stop()
				if c.isHovered {
					xR := ceil(c.pos.X)
					xL := xR - float32(gtx.Dp(1))
					children := []layout.FlexChild{}
					values := []float64{}
					for i := range c.Series {
						i := i
						if !c.Enabled[i].Value {
							continue
						}
						idx := sort.Search(len(c.seriesSlices[i]), func(idx int) bool {
							return c.seriesSlices[i][idx].xR <= xR
						})
						if idx == len(c.seriesSlices[i]) {
							continue
						}
						data := c.seriesSlices[i][idx]
						insertIdx, _ := slices.BinarySearch(values, data.mean)
						values = slices.Insert(values, insertIdx, data.mean)
						children = slices.Insert(children, len(children)-insertIdx, layout.Rigid(func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(material.Body2(th, strconv.FormatFloat(data.mean, 'f', 3, 64)).Layout),
								layout.Rigid(layout.Spacer{Width: 8}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									size := image.Pt(gtx.Dp(8), gtx.Dp(8))
									paint.FillShape(gtx.Ops, colors[i], clip.Ellipse{Max: size}.Op(gtx.Ops))
									return D{Size: size}
								}),
							)
						}))
					}
					origConstraints := gtx.Constraints
					gtx.Constraints.Min = image.Point{}
					hoverInfoMacro := op.Record(gtx.Ops)
					hoverInfoDims := layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 150}, clip.Rect{Max: gtx.Constraints.Min}.Op())
							return D{Size: gtx.Constraints.Min}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(10).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{
									Axis:      layout.Vertical,
									Alignment: layout.End,
								}.Layout(gtx, children...)
							})
						},
					)
					hoverInfoCall := hoverInfoMacro.Stop()
					gtx.Constraints = origConstraints

					pos := image.Point{}
					if int(xL) > gtx.Constraints.Max.X-int(xR) {
						pos.X = max(int(xL)-hoverInfoDims.Size.X, 0)
					} else {
						pos.X = min(int(xR), gtx.Constraints.Max.X-hoverInfoDims.Size.X)
					}
					if offscreenY := gtx.Constraints.Max.Y - (int(c.pos.Y) + hoverInfoDims.Size.Y); offscreenY < 0 {
						pos.Y = int(c.pos.Y) + offscreenY
					} else {
						pos.Y = int(c.pos.Y)
					}
					call.Add(gtx.Ops)
					paint.FillShape(gtx.Ops, color.NRGBA{A: 255}, clip.Rect{
						Min: image.Point{
							X: int(xL),
						},
						Max: image.Point{
							X: int(xR),
							Y: gtx.Constraints.Max.Y,
						},
					}.Op())
					transform := op.Offset(pos).Push(gtx.Ops)
					hoverInfoCall.Add(gtx.Ops)
					transform.Pop()
				} else {
					call.Add(gtx.Ops)
				}
				return D{Size: gtx.Constraints.Max}
			}),
			layout.Expanded(func(gtx C) D {
				scrollbar := material.Scrollbar(th, &c.panBar)
				scrollbar.Track.MajorPadding = 0
				scrollbar.Track.MinorPadding = 0
				scrollbar.Indicator.CornerRadius = 0
				scrollbar.Indicator.Color.A = 100
				return scrollbar.Layout(gtx, layout.Horizontal, vpStart, vpEnd)
			}),
		)
	})

	return dims, visibleDomainStart, visibleDomainEnd, pxPerWatt, rangeMin, rangeMax
}

func ceil[T constraints.Integer | constraints.Float](a T) T {
	return T(math.Ceil(float64(a)))
}

func floor[T constraints.Integer | constraints.Float](a T) T {
	return T(math.Floor(float64(a)))
}

func (c *ChartData) computeRange(gtx C) (maxY, pxPerWatt int, rangeMax float64) {
	maxY = gtx.Constraints.Max.Y - gtx.Dp(1)
	maxYDp := gtx.Metric.PxToDp(gtx.Constraints.Max.Y)
	var (
		rangeSum float64
	)

	for i, series := range c.Series {
		if !c.Enabled[i].Value {
			continue
		}
		rangeMax = max(rangeMax, series.RangeRateMax)
		rangeSum += series.RangeRateMax
	}
	if c.Stacked.Value {
		rangeMax = rangeSum
	}
	rangeMax = ceil(rangeMax)
	dPPerWatt := max(floor(maxYDp/unit.Dp(rangeMax)), 1)
	pxPerWatt = gtx.Dp(dPPerWatt)
	// Add back any pixels that weren't used by our power-of-ten scaling.
	rangeMax += (float64(maxY) - float64(pxPerWatt)*rangeMax) / float64(pxPerWatt)
	return maxY, pxPerWatt, rangeMax
}

func (c *ChartData) computeVisible(gtx C, maxY int, domainMin, domainMax int64, rangeMax float64) {

	rangeMin := float64(0)
	rangeInterval := float32(rangeMax - rangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}

	oneDp := float32(gtx.Dp(1))
	totalIntervals := int(ceil(gtx.Metric.PxToDp(gtx.Constraints.Max.X)))
	for i, series := range c.Series {
		if c.Enabled[i].Value {
			c.seriesSlices[i] = c.seriesSlices[i][:0]
			intervalMean := 0.0
			for intervalCount := 1; intervalCount <= totalIntervals; intervalCount++ {
				tsStart := domainMax - (c.nsPerDp * int64(intervalCount))
				tsEnd := tsStart + c.nsPerDp
				var ok bool
				_, intervalMean, _, ok = series.RatesBetween(tsStart, tsEnd)
				if !ok {
					continue
				}

				// Compute the X and Y coordinates for this portion of the data.
				xL := float32(gtx.Constraints.Max.X) - float32(gtx.Dp(unit.Dp(intervalCount)))
				xR := xL + oneDp

				yT := float32(maxY) - (float32(intervalMean-rangeMin)/rangeInterval)*float32(maxY)

				// record this data in the series for the hover dialog.
				c.seriesSlices[i] = append(c.seriesSlices[i], timeslice{
					tsStart: tsStart,
					tsEnd:   tsEnd,
					xL:      xL,
					xR:      xR,
					y:       yT,
					mean:    intervalMean,
				})

			}
		}
	}
}

func (c *ChartData) layoutYAxisGrid(gtx C, maxY, pxPerWatt int) {
	oneDp := float32(gtx.Dp(1))
	for gridNum := 0; gridNum*pxPerWatt < maxY; gridNum++ {
		yT := maxY - gridNum*pxPerWatt
		a := uint8(50)
		if gridNum%10 == 0 {
			a = 100
		}
		paint.FillShape(gtx.Ops, color.NRGBA{A: a}, clip.Rect{
			Min: image.Point{
				Y: yT,
			},
			Max: image.Point{
				Y: yT + int(oneDp),
				X: gtx.Constraints.Max.X,
			},
		}.Op())
	}
}

func (c *ChartData) layoutLinePlot(gtx C, maxY, pxPerWatt int, rangeMax float64) {
	rangeMin := float64(0)
	rangeInterval := float32(rangeMax - rangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}

	oneDp := float32(gtx.Dp(1))

	for i := range c.Series {
		if c.Enabled[i].Value {
			c.returnPath = c.returnPath[:0]
			var p clip.Path
			p.Begin(gtx.Ops)
			prevIntervalMean := 0.0
			prevYT := float32(0)
			prevYB := float32(0)
			for dataIndex, seriesData := range c.seriesSlices[i] {
				intervalMean := seriesData.mean
				xR := seriesData.xR
				yT := seriesData.y
				yB := yT + oneDp
				var nextIntervalMean float64
				if dataIndex < len(c.seriesSlices[i])-1 {
					nextIntervalMean = c.seriesSlices[i][dataIndex+1].mean
				}
				nextYT := float32(maxY) - (float32(nextIntervalMean-rangeMin)/rangeInterval)*float32(maxY)
				if nextYT > yT || prevYT > yT {
					yB = max(nextYT+oneDp, prevYT+oneDp)
				}

				if intervalMean == prevIntervalMean &&
					nextIntervalMean == intervalMean &&
					dataIndex > 0 &&
					dataIndex < len(c.seriesSlices[i])-1 &&
					prevYB-prevYT == oneDp {
					// We can safely skip processing the current interval if it
					// has the same value as the previous and next intervals,
					// is neither the first nor the last interval in the graph,
					// and the previous segment had the default line thickness.
					continue
				}

				if dataIndex == 0 {
					// The very first interval needs to add special path segments.
					p.MoveTo(f32.Pt(xR, yT))
					prevYT = yT
				}
				p.LineTo(f32.Pt(xR, prevYT))
				p.LineTo(f32.Pt(xR, yT))
				c.returnPath = append(c.returnPath,
					f32.Pt(xR, prevYB),
					f32.Pt(xR, yB),
				)
				prevYT = yT
				prevYB = yB
				prevIntervalMean = intervalMean
			}

			for i := range c.returnPath {
				p.LineTo(c.returnPath[len(c.returnPath)-(i+1)])
			}
			p.Close()

			stack := clip.Outline{
				Path: p.End(),
			}.Op().Push(gtx.Ops)
			paint.Fill(gtx.Ops, colors[i%len(colors)])
			stack.Pop()
		}
	}
}

func (c *ChartData) layoutStackPlot(gtx C, maxY, pxPerWatt int, rangeMax float64) {
	rangeMin := float64(0)
	rangeInterval := float32(rangeMax - rangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}
	stackSums := make([]float64, len(c.seriesSlices[0]))
	layers := make([]op.CallOp, 0, len(c.Series))
	for i := 0; i < len(c.Series); i++ {
		if c.Enabled[i].Value {
			macro := op.Record(gtx.Ops)
			var p clip.Path
			p.Begin(gtx.Ops)
			// Build the path for the top of the area.
			for sampleIdx, sample := range c.seriesSlices[i] {
				datum := sample.mean
				xR := sample.xR
				xL := sample.xL
				datumY := float32(gtx.Constraints.Max.Y) - (float32(datum+stackSums[sampleIdx]-rangeMin)/rangeInterval)*float32(gtx.Constraints.Max.Y)
				ptR := f32.Pt(xR, datumY)
				ptL := f32.Pt(xL, datumY)
				if sampleIdx == 0 {
					p.MoveTo(ptR)
				} else {
					p.LineTo(ptR)
				}
				p.LineTo(ptL)
				stackSums[sampleIdx] += datum
			}
			p.LineTo(f32.Pt(0, float32(gtx.Constraints.Max.Y)))
			p.LineTo(layout.FPt(gtx.Constraints.Max))
			p.Close()

			stack := clip.Outline{
				Path: p.End(),
			}.Op().Push(gtx.Ops)
			paint.Fill(gtx.Ops, colors[i%len(colors)])
			stack.Pop()
			layers = append(layers, macro.Stop())
		}
	}
	for i := len(layers) - 1; i >= 0; i-- {
		layers[i].Add(gtx.Ops)
	}
}
