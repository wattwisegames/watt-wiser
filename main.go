package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/outlay"
	"golang.org/x/exp/constraints"
)

type Sample struct {
	StartTimestampNS, EndTimestampNS int64
	Data                             []float64
}

func main() {
	var traceInto string
	flag.StringVar(&traceInto, "trace", "", "collect a go runtime trace into the given file")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `%[1]s: visualize a csv energy trace file
Usage:

 %[1]s [flags] <file>

OR

 watt-wiser-sensors | %[1]s [flags]

Flags:
`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	var f *os.File
	if traceInto != "" {
		pprof.StartCPUProfile(io.Discard)
		var err error
		f, err = os.Create(traceInto)
		if err != nil {
			log.Printf("failed creating trace file: %v", err)
		} else {
			trace.Start(f)
		}
	}
	go func() {
		var source io.Reader = os.Stdin
		if flag.NArg() > 0 {
			f, err := os.Open(flag.Arg(0))
			if err != nil {
				log.Printf("failed opening %q, falling back to stdin: %v", flag.Arg(0), err)
			}
			defer f.Close()
			source = f
		}
		csvReader := csv.NewReader(source)
		csvReader.TrimLeadingSpace = true
		headings, err := csvReader.Read()
		if err != nil {
			log.Fatalf("could not read csv headings: %v", err)
		}
		samplesChan := make(chan Sample, 1024)
		relevantIndices := make([]int, 2, len(headings))
		relevantIndices[0] = 0
		relevantIndices[1] = 1
		relevantHeadings := make([]string, 0, len(headings))
		for i, heading := range headings {
			if i == 0 {
				continue
			}
			if strings.Contains(heading, "(J)") {
				relevantIndices = append(relevantIndices, i)
				relevantHeadings = append(relevantHeadings, heading)
			}
		}

		w := app.NewWindow(app.Title("Watt Wiser"))
		go func() {
			err := loop(w, relevantHeadings, samplesChan)
			if traceInto != "" {
				trace.Stop()
				f.Close()
				pprof.StopCPUProfile()
			}
			if err != nil {
				log.Fatal(err)
			}
			os.Exit(0)
		}()

		// Continously parse the CSV data and send it on the channel.
		for {
			rec, err := csvReader.Read()
			if err != nil {
				log.Printf("could not read sensor data: %v", err)
				return
			}
			samples := make([]float64, len(relevantIndices)-2)
			startNs, err := strconv.ParseInt(rec[0], 10, 64)
			if err != nil {
				log.Printf("failed parsing timestamp: %v", err)
				continue
			}
			endNs, err := strconv.ParseInt(rec[1], 10, 64)
			if err != nil {
				log.Printf("failed parsing timestamp: %v", err)
				continue
			}
			for i := 2; i < len(relevantIndices); i++ {
				data, err := strconv.ParseFloat(rec[relevantIndices[i]], 64)
				if err != nil {
					log.Printf("failed parsing data[%d]=%q: %v", i, rec[i], err)
					continue
				}
				samples[i-2] = data
			}
			samplesChan <- Sample{
				StartTimestampNS: startNs,
				EndTimestampNS:   endNs,
				Data:             samples,
			}
		}
	}()

	app.Main()
}

type (
	C = layout.Context
	D = layout.Dimensions
)

type timeslice struct {
	tsStart, tsEnd int64
	xL, xR         float32
	y              float32
	mean           float64
}

type ChartData struct {
	DomainMin    int64
	DomainMax    int64
	Series       []Series
	seriesSlices [][]timeslice
	Headings     []string
	Enabled      []*widget.Bool
	Stacked      widget.Bool
	zoom         gesture.Scroll
	pan          gesture.Scroll
	panBar       widget.Scrollbar
	xOffset      int64
	nsPerDp      int64
	// returnPath is a scratch slice used to build each data series'
	// path.
	returnPath []f32.Point
	// hover gesture state
	pos       f32.Point
	isHovered bool
}

func (c *ChartData) Insert(sample Sample) {
	if len(c.Series) == 0 {
		c.DomainMin = sample.StartTimestampNS
		c.DomainMax = sample.StartTimestampNS
		c.Series = make([]Series, len(sample.Data))
		c.Enabled = make([]*widget.Bool, len(sample.Data))
		c.seriesSlices = make([][]timeslice, len(sample.Data))
		for i := range c.Enabled {
			c.Enabled[i] = new(widget.Bool)
			c.Enabled[i].Value = true
		}
		c.nsPerDp = 10_000_000 // ns/Dp
	}
	for i, datum := range sample.Data {
		// RangeMin should probably always be zero, no matter what the sensors say. None of the
		// quantities we're measuring can actually be less than zero.
		//c.RangeMin = min(datum, c.RangeMin)
		if datum < 0 {
			datum = 0
		}
		c.Series[i].Insert(sample.StartTimestampNS, sample.EndTimestampNS, datum)
	}
	c.DomainMin = min(sample.StartTimestampNS, c.DomainMin)
	c.DomainMax = max(sample.StartTimestampNS, c.DomainMax)
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

	axisDims := layout.Flex{
		Axis:      layout.Vertical,
		Alignment: layout.Middle,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, fmt.Sprintf("Power Draw in Watts (scale = %.1f Dp/Watt)", gtx.Metric.PxToDp(pxPerWatt))).Layout(gtx)
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

func (c *ChartData) Layout(gtx C, th *material.Theme) D {
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
	keyDims := c.layoutKey(gtx, th)
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
	minDomainLabel := material.Body1(th, strconv.FormatFloat(domainStartSecs, 'f', 3, 64)+" seconds")
	maxDomainLabel := material.Body1(th, strconv.FormatFloat(domainEndSecs, 'f', 3, 64)+" seconds")
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
							return D{Size: image.Point{
								Y: axisLabelDims.Size.Y,
							}}
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
							return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
								layout.Rigid(minDomainLabel.Layout),
								layout.Rigid(material.Body2(th, fmt.Sprintf("Time (spans %.2f s, scale = %d ns/Dp)", domainIntervalSecs, c.nsPerDp)).Layout),
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

func (c *ChartData) layoutKey(gtx C, th *material.Theme) D {
	return outlay.FlowWrap{}.Layout(gtx, len(c.Headings)+1, func(gtx layout.Context, i int) layout.Dimensions {
		return layout.UniformInset(8).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if i == len(c.Headings) {
				sum := 0.0
				for sumIdx, series := range c.Series {
					if c.Enabled[sumIdx].Value {
						sum += series.Sum
					}
				}
				return material.Body2(th, fmt.Sprintf("Total recorded: %.2f J", sum)).Layout(gtx)
			}
			c.Enabled[i].Update(gtx)
			enabled := c.Enabled[i].Value
			disabledAlpha := uint8(100)
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sideLen := gtx.Dp(10)
					sz := image.Pt(sideLen, sideLen)
					fullColor := colors[i]
					if !enabled {
						fullColor.A = disabledAlpha
					}
					paint.FillShape(gtx.Ops, fullColor, clip.Rect{Max: sz}.Op())
					return c.Enabled[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return D{Size: sz}
					})
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					l := material.Body2(th, c.Headings[i])
					if !enabled {
						l.Color.A = disabledAlpha
					}
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					l := material.Body2(th, fmt.Sprintf("%.2f J", c.Series[i].Sum))
					if !enabled {
						l.Color.A = disabledAlpha
					}
					return l.Layout(gtx)
				}),
			)
		})
	})
}

func (c *ChartData) layoutPlot(gtx C, th *material.Theme) (dims D, domainMin, domainMax int64, pxPerWatt int, rangeMin, rangeMax float64) {
	c.Stacked.Update(gtx)
	for _, ev := range gtx.Events(c) {
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
	dist := c.zoom.Update(gtx.Metric, gtx.Queue, gtx.Now, gesture.Vertical)
	if dist != 0 {
		proportion := 1 + float64(dist)/float64(gtx.Constraints.Max.Y)
		c.nsPerDp = int64(math.Round(float64(c.nsPerDp) * proportion))
	}
	var pannedNS int64
	dist = c.pan.Update(gtx.Metric, gtx.Queue, gtx.Now, gesture.Horizontal)
	if dist != 0 {
		pannedNS += int64(gtx.Metric.PxToDp(dist) * unit.Dp(c.nsPerDp))
	}
	totalDomainInterval := c.DomainMax - c.DomainMin
	if panDist := c.panBar.ScrollDistance(); panDist != 0 {
		pannedNS += int64(panDist * float32(totalDomainInterval))
	}
	if pannedNS != 0 {
		if c.xOffset+pannedNS <= 0 && c.DomainMax+c.xOffset+pannedNS >= c.DomainMin {
			c.xOffset += pannedNS
		} else if c.xOffset+pannedNS > 0 {
			c.xOffset = 0
		}
	}
	numDp := gtx.Metric.PxToDp(gtx.Constraints.Max.X)
	visibleDomainInterval := int64(math.Round(float64(numDp * unit.Dp(c.nsPerDp))))
	// visibleDomainEnd forces the first datapoint to be an even multiple of the current
	// scale, which prevents weird cross-frame sampling artifacts.
	visibleDomainEnd := ((c.DomainMax + c.xOffset) / c.nsPerDp) * c.nsPerDp
	visibleDomainStart := visibleDomainEnd - visibleDomainInterval
	var maxY int
	maxY, pxPerWatt, rangeMax = c.computeRange(gtx)
	c.computeVisible(gtx, maxY, visibleDomainStart, visibleDomainEnd, rangeMax)

	dims = c.Stacked.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.S}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				macro := op.Record(gtx.Ops)
				c.pan.Add(gtx.Ops, image.Rect(-1e6, 0, 1e6, 0))
				c.zoom.Add(gtx.Ops, image.Rect(0, -1e6, 0, 1e6))
				pointer.InputOp{
					Tag:   c,
					Kinds: pointer.Enter | pointer.Leave | pointer.Move,
				}.Add(gtx.Ops)
				// Draw grid underneath plot.
				c.layoutYAxisGrid(gtx, maxY, pxPerWatt)
				if !c.Stacked.Value {
					c.layoutLinePlot(gtx, visibleDomainStart, visibleDomainEnd, maxY, pxPerWatt, rangeMax)
				} else {
					c.layoutStackPlot(gtx, visibleDomainStart, visibleDomainEnd, maxY, pxPerWatt, rangeMax)
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
				end := visibleDomainEnd - c.DomainMin
				start := visibleDomainStart - c.DomainMin
				vpStart := float32(start) / float32(totalDomainInterval)
				vpEnd := float32(end) / float32(totalDomainInterval)
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
	// ensure the range max is a power of ten.
	rangeMax = 10 * ceil(rangeMax*.1)
	dPPerWatt := floor(maxYDp / unit.Dp(rangeMax))
	pxPerWatt = gtx.Dp(dPPerWatt)
	// Add back any pixels that weren't used by our power-of-ten scaling.
	rangeMax += (float64(maxY) - float64(pxPerWatt)*rangeMax) / float64(pxPerWatt)
	return maxY, pxPerWatt, rangeMax
}

func (c *ChartData) computeVisible(gtx C, maxY int, domainMin, domainMax int64, rangeMax float64) {
	nanosPerPx := int64(math.Round(float64(c.nsPerDp) / float64(gtx.Metric.PxPerDp)))

	rangeMin := float64(0)
	rangeInterval := float32(rangeMax - rangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}

	oneDp := float32(gtx.Dp(1))
	totalIntervals := gtx.Constraints.Max.X
	for i, series := range c.Series {
		if c.Enabled[i].Value {
			c.seriesSlices[i] = c.seriesSlices[i][:0]
			c.returnPath = c.returnPath[:0]
			intervalMean := 0.0
			for intervalCount := 1; intervalCount <= totalIntervals; intervalCount++ {
				tsStart := domainMax - (nanosPerPx * int64(intervalCount))
				tsEnd := tsStart + nanosPerPx
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

func (c *ChartData) layoutLinePlot(gtx C, domainMin, domainMax int64, maxY, pxPerWatt int, rangeMax float64) {
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
				intervalMean = nextIntervalMean
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

func (c *ChartData) layoutStackPlot(gtx C, domainMin, domainMax int64, maxY, pxPerWatt int, rangeMax float64) {
	domainInterval := float32(domainMax - domainMin)
	if domainInterval == 0 {
		domainInterval = 1
	}
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

func loop(w *app.Window, headings []string, samples chan Sample) error {
	var dataMutex sync.Mutex
	var data ChartData
	data.Headings = headings
	var ops op.Ops
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	go func() {
		for sample := range samples {
			func() {
				dataMutex.Lock()
				defer dataMutex.Unlock()
				data.Insert(sample)
			}()
			w.Invalidate()
		}
	}()
	for {
		switch ev := w.NextEvent().(type) {
		case system.DestroyEvent:
			return ev.Err
		case system.FrameEvent:
			gtx := layout.NewContext(&ops, ev)
			func() {
				dataMutex.Lock()
				defer dataMutex.Unlock()
				data.Layout(gtx, th)
			}()
			ev.Frame(gtx.Ops)
		}
	}
}
