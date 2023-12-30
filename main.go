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
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
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
	"git.sr.ht/~whereswaldon/energy/hwmon"
	"git.sr.ht/~whereswaldon/energy/rapl"
	"git.sr.ht/~whereswaldon/energy/sensors"
	"golang.org/x/exp/constraints"
)

func sensorsMain() {
	raplWatches, err := rapl.FindRAPL()
	if err != nil {
		log.Fatal(err)
	}
	relevantSubfeatures, err := hwmon.FindEnergySensors()
	if err != nil {
		log.Fatal(err)
	}
	sensorList := make([]sensors.Sensor, 0, len(raplWatches)+len(relevantSubfeatures))
	for _, w := range raplWatches {
		sensorList = append(sensorList, w)
	}
	for _, s := range relevantSubfeatures {
		sensorList = append(sensorList, s)
	}
	fmt.Printf("sample start (ns), sample end (ns), ")
	for _, s := range sensorList {
		fmt.Printf("%s (%s), ", s.Name(), s.Unit())
		if s.Unit() == sensors.Watts {
			fmt.Printf("integrated %s (%s),", s.Name(), sensors.Joules)
		}
	}
	fmt.Println()
	lastReadTime := time.Now()
	// Pre-read every sensor once to ensure that incremental sensors emit coherent first values.
	for _, chip := range sensorList {
		_, err := chip.Read()
		if err != nil {
			log.Fatalf("failed reading value: %v", err)
			return
		}
	}
	sampleRate := time.Millisecond * 100
	sampleRateSeconds := float64(sampleRate) / float64(time.Second)
	ticker := time.NewTicker(sampleRate)
	defer ticker.Stop()
	for {
		select {
		case t := <-ticker.C:
			fmt.Printf("%d, %d, ", lastReadTime.UnixNano(), t.UnixNano())
			for _, chip := range sensorList {
				v, err := chip.Read()
				if err != nil {
					log.Fatalf("failed reading value: %v", err)
					return
				}
				fmt.Printf("%f, ", v)
				if chip.Unit() == sensors.Watts {
					fmt.Printf("%f, ", v*sampleRateSeconds)
				}
			}
			fmt.Println()
			lastReadTime = t
		}
	}
}

type Sample struct {
	StartTimestampNS, EndTimestampNS int64
	Data                             []float64
}

func main() {
	runSensors := flag.Bool("sensors", false, "read sensor data and emit as CSV on stdout")
	flag.Parse()
	if *runSensors {
		sensorsMain()
		return
	}
	pprof.StartCPUProfile(io.Discard)
	f, err := os.Create("trace.trace")
	if err != nil {
		log.Printf("failed creating trace file: %v", err)
	} else {
		trace.Start(f)
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

		w := app.NewWindow()
		go func() {
			err := loop(w, relevantHeadings, samplesChan)
			trace.Stop()
			f.Close()
			pprof.StopCPUProfile()
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

type ChartData struct {
	RangeMin  float64
	RangeMax  float64
	DomainMin int64
	DomainMax int64
	Series    []Series
	Headings  []string
	Enabled   []*widget.Bool
	Stacked   widget.Bool
	scroll    gesture.Scroll
	nsPerDp   int64
}

func (c *ChartData) Insert(sample Sample) {
	intervalNs := float64(sample.EndTimestampNS - sample.StartTimestampNS)
	intervalSecs := intervalNs / 1_000_000_000
	if len(c.Series) == 0 {
		c.DomainMin = sample.StartTimestampNS
		c.DomainMax = sample.StartTimestampNS
		c.RangeMax = sample.Data[0] / intervalSecs
		c.Series = make([]Series, len(sample.Data))
		c.Enabled = make([]*widget.Bool, len(sample.Data))
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
		c.RangeMax = max(datum/intervalSecs, c.RangeMax)
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
	gtx.Constraints = origConstraints.SubMax(axisLabelDims.Size.Add(image.Pt(0, keyDims.Size.Y)))
	macro = op.Record(gtx.Ops)
	dims, domainMin, domainMax, rangeMin, rangeMax := c.layoutPlot(gtx)
	domainIntervalSecs := float32(domainMax-domainMin) / 1_000_000_000
	plotCall := macro.Stop()
	gtx.Constraints = origConstraints
	minRangeLabel.Text = strconv.FormatFloat(rangeMin, 'f', 3, 64)
	maxRangeLabel := material.Body1(th, strconv.FormatFloat(rangeMax, 'f', 3, 64))
	minDomainLabel := material.Body1(th, strconv.FormatInt(domainMin, 10))
	maxDomainLabel := material.Body1(th, strconv.FormatInt(domainMax, 10))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(maxRangeLabel.Layout),
						layout.Flexed(1, func(gtx C) D {
							return D{Size: gtx.Constraints.Min}
						}),
						layout.Rigid(material.Body2(th, "Watts").Layout),
						layout.Flexed(1, func(gtx C) D {
							return D{Size: gtx.Constraints.Min}
						}),
						layout.Rigid(minRangeLabel.Layout),
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
								layout.Rigid(material.Body2(th, fmt.Sprintf("ns timestamp (%.3fs) scale = %dns/Dp", domainIntervalSecs, c.nsPerDp)).Layout),
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

func (c *ChartData) layoutPlot(gtx C) (dims D, domainMin, domainMax int64, rangeMin, rangeMax float64) {
	rangeMin = c.RangeMin
	c.Stacked.Update(gtx)
	dist := c.scroll.Update(gtx.Metric, gtx.Queue, gtx.Now, gesture.Vertical)
	if dist != 0 {
		proportion := 1 + float64(dist)/float64(gtx.Constraints.Max.Y)
		c.nsPerDp = int64(math.Round(float64(c.nsPerDp) * proportion))
	}
	dims = c.Stacked.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		c.scroll.Add(gtx.Ops, image.Rect(0, -1e6, 0, 1e6))
		if !c.Stacked.Value {
			domainMin, domainMax, rangeMin, rangeMax = c.layoutLinePlot(gtx)
		} else {
			//			rangeMax = c.layoutStackPlot(gtx, visibleSamples, domainMin, domainMax)
		}
		return D{Size: gtx.Constraints.Max}
	})
	return dims, domainMin, domainMax, rangeMin, rangeMax
}

func ceil[T constraints.Integer | constraints.Float](a T) T {
	return T(math.Ceil(float64(a)))
}

func floor[T constraints.Integer | constraints.Float](a T) T {
	return T(math.Floor(float64(a)))
}

func (c *ChartData) layoutLinePlot(gtx C) (domainMin, domainMax int64, rangeMin, rangeMax float64) {
	numDp := gtx.Metric.PxToDp(gtx.Constraints.Max.X)
	nanosPerPx := int64(math.Round(float64(c.nsPerDp) / float64(gtx.Metric.PxPerDp)))
	domainInterval := int64(math.Round(float64(numDp * unit.Dp(c.nsPerDp))))
	rangeInterval := float32(c.RangeMax - c.RangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}
	oneDp := float32(gtx.Dp(1))
	// domainEnd forces the first datapoint to be an even multiple of the current
	// scale, which prevents weird cross-frame sampling artifacts.
	domainEnd := (c.DomainMax / c.nsPerDp) * c.nsPerDp
	domainStart := domainEnd - domainInterval
	totalIntervals := gtx.Constraints.Max.X
	for i, series := range c.Series {
		if c.Enabled[i].Value {
			var p clip.Path
			p.Begin(gtx.Ops)
			returnPath := []f32.Point{}
			prevIntervalMean := 0.0
			prevYT := float32(0)
			for intervalCount := 1; intervalCount <= totalIntervals; intervalCount++ {
				tsStart := domainEnd - (nanosPerPx * int64(intervalCount))
				tsEnd := tsStart + nanosPerPx
				_, intervalMean, _, ok := series.Values(tsStart, tsEnd)
				if !ok {
					continue
				}
				if intervalMean == prevIntervalMean && intervalCount > 1 && intervalCount < totalIntervals {
					// skip adding path elements that do not change the Y coordinate.
					continue
				}
				prevIntervalMean = intervalMean

				xL := floor((float32(tsStart-domainStart) / float32(domainInterval)) * float32(gtx.Constraints.Max.X))
				xR := xL + float32(gtx.Dp(1))

				yT := float32(gtx.Constraints.Max.Y) - (float32(intervalMean-c.RangeMin)/rangeInterval)*float32(gtx.Constraints.Max.Y)
				yB := yT + float32(gtx.Dp(1))
				if intervalCount == 1 {
					// The very first interval needs to add special path segments.
					p.MoveTo(f32.Pt(xR, yT+oneDp))
					p.LineTo(f32.Pt(xR, yT))
				} else if prevYT > yT {
					p.LineTo(f32.Pt(xR, prevYT))
					p.LineTo(f32.Pt(xR, yT))
					returnPath = append(returnPath,
						f32.Pt(xL, prevYT+oneDp),
						f32.Pt(xL, yB),
					)
				} else if prevYT < yT {
					p.LineTo(f32.Pt(xL, prevYT))
					p.LineTo(f32.Pt(xL, yT))
					returnPath = append(returnPath,
						f32.Pt(xR, prevYT+oneDp),
						f32.Pt(xR, yB),
					)
				}
				prevYT = yT
			}
			for i := range returnPath {
				p.LineTo(returnPath[len(returnPath)-(i+1)])
			}
			p.Close()

			stack := clip.Outline{
				Path: p.End(),
			}.Op().Push(gtx.Ops)
			paint.Fill(gtx.Ops, colors[i%len(colors)])
			stack.Pop()
		}
	}
	return domainStart, domainEnd, 0, c.RangeMax
}

/*
func (c *ChartData) layoutStackPlot(gtx C, visibleSamples []Sample, domainMin, domainMax int64) (rangeMax float64) {
	domainInterval := float32(domainMax - domainMin)
	if domainInterval == 0 {
		domainInterval = 1
	}
	stackRangeMax := 0.0
	for i := range c.Enabled {
		if c.Enabled[i].Value {
			stackRangeMax += c.DatasetRanges[i].Max
		}
	}
	rangeInterval := float32(stackRangeMax - c.RangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}
	stackSums := make([]float64, len(visibleSamples))
	layers := make([]op.CallOp, 0, len(c.Samples[0].Data))
	for i := 0; i < len(c.Samples[0].Data); i++ {
		if c.Enabled[i].Value {
			macro := op.Record(gtx.Ops)
			var p clip.Path
			p.Begin(gtx.Ops)
			// Build the path for the top of the area.
			for sampleIdx, sample := range visibleSamples {
				datum := sample.Data[i]
				x := (float32(sample.TimestampNS-domainMin) / domainInterval) * float32(gtx.Constraints.Max.X)
				datumY := float32(gtx.Constraints.Max.Y) - (float32(datum+stackSums[sampleIdx]-c.RangeMin)/rangeInterval)*float32(gtx.Constraints.Max.Y)
				pt := f32.Pt(x, datumY)
				if sampleIdx == 0 {
					p.MoveTo(pt)
				} else {
					p.LineTo(f32.Pt(x, datumY))
				}
				stackSums[sampleIdx] += datum
			}
			p.LineTo(layout.FPt(gtx.Constraints.Max))
			p.LineTo(f32.Pt(0, float32(gtx.Constraints.Max.Y)))
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
	return stackRangeMax
}
*/

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
