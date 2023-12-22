package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget/material"
	"gioui.org/x/outlay"
	"git.sr.ht/~whereswaldon/energy/hwmon"
	"git.sr.ht/~whereswaldon/energy/rapl"
	"git.sr.ht/~whereswaldon/energy/sensors"
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
	fmt.Printf("timestamp_ns, ")
	for _, s := range sensorList {
		fmt.Printf("%s (%s), ", s.Name(), s.Unit())
		if s.Unit() == sensors.Watts {
			fmt.Printf("integrated %s (%s),", s.Name(), sensors.Joules)
		}
	}
	fmt.Println()
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
			fmt.Printf("%d, ", t.UnixNano())
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
		}
	}
}

type Sample struct {
	TimestampNS int64
	Data        []float64
}

func main() {
	runSensors := flag.Bool("sensors", false, "read sensor data and emit as CSV on stdout")
	flag.Parse()
	if *runSensors {
		sensorsMain()
		return
	}
	go func() {
		csvReader := csv.NewReader(os.Stdin)
		csvReader.TrimLeadingSpace = true
		headings, err := csvReader.Read()
		if err != nil {
			log.Fatalf("could not read csv headings: %v", err)
		}
		samplesChan := make(chan Sample, 1024)
		relevantIndices := make([]int, 1, len(headings))
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
			if err := loop(w, relevantHeadings, samplesChan); err != nil {
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
			samples := make([]float64, len(relevantIndices)-1)
			ns, err := strconv.ParseInt(rec[0], 10, 64)
			if err != nil {
				log.Printf("failed parsing timestamp: %v", err)
				continue
			}
			for i := 1; i < len(relevantIndices); i++ {
				data, err := strconv.ParseFloat(rec[relevantIndices[i]], 64)
				if err != nil {
					log.Printf("failed parsing data[%d]=%q: %v", i, rec[i], err)
					continue
				}
				samples[i-1] = data
			}
			samplesChan <- Sample{
				TimestampNS: ns,
				Data:        samples,
			}
			w.Invalidate()
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
	Samples   []Sample
	Sums      []float64
	Headings  []string
}

func (c *ChartData) Insert(sample Sample) {
	if len(c.Samples) == 0 {
		c.DomainMin = sample.TimestampNS
		c.DomainMax = sample.TimestampNS
		c.RangeMax = sample.Data[0]
		c.Sums = make([]float64, len(sample.Data))
	}
	for i, datum := range sample.Data {
		c.RangeMin = min(datum, c.RangeMin)
		c.RangeMax = max(datum, c.RangeMax)
		c.Sums[i] += datum
	}
	c.DomainMin = min(sample.TimestampNS, c.DomainMin)
	c.DomainMax = max(sample.TimestampNS, c.DomainMax)
	c.Samples = append(c.Samples, sample)
}

var colors = []color.NRGBA{
	{R: 0xa4, G: 0x63, B: 0x3a, A: 0xff},
	{R: 0x85, G: 0x76, B: 0x25, A: 0xff}, //#857625
	{R: 0x51, G: 0x85, B: 0x4d, A: 0xff}, //#51854d
	{R: 0x2b, G: 0x7f, B: 0xa8, A: 0xff}, //#2b7fa8
	{R: 0x72, G: 0x6c, B: 0xae, A: 0xff}, //#726cae
	{R: 0x97, G: 0x5f, B: 0x91, A: 0xff}, //975f91
}

func (c *ChartData) Layout(gtx C, th *material.Theme) D {
	if len(c.Samples) < 1 {
		return D{Size: gtx.Constraints.Max}
	}
	minRangeLabel := material.Body1(th, strconv.FormatFloat(c.RangeMin, 'f', 3, 64))
	maxRangeLabel := material.Body1(th, strconv.FormatFloat(c.RangeMax, 'f', 3, 64))
	origConstraints := gtx.Constraints
	gtx.Constraints.Min = image.Point{}
	macro := op.Record(gtx.Ops)
	domainDims := minRangeLabel.Layout(gtx)
	_ = macro.Stop()
	gtx.Constraints = origConstraints
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(maxRangeLabel.Layout),
						layout.Flexed(1, func(gtx C) D {
							return D{Size: gtx.Constraints.Min}
						}),
						layout.Rigid(material.Body2(th, "Joules\nper\nsample").Layout),
						layout.Flexed(1, func(gtx C) D {
							return D{Size: gtx.Constraints.Min}
						}),
						layout.Rigid(minRangeLabel.Layout),
						layout.Rigid(func(gtx C) D {
							return D{Size: image.Point{
								Y: domainDims.Size.Y,
							}}
						}),
					)
				}),
				layout.Flexed(1, func(gtx C) D {
					origConstraints = gtx.Constraints
					gtx.Constraints = gtx.Constraints.SubMax(image.Point{0, domainDims.Size.Y})
					macro := op.Record(gtx.Ops)
					dims, domainMin, domainMax := c.layoutPlot(gtx)
					domainIntervalSecs := float32(domainMax-domainMin) / 1_000_000_000
					call := macro.Stop()
					gtx.Constraints = origConstraints
					minDomainLabel := material.Body1(th, "+"+strconv.FormatInt(domainMin, 10))
					maxDomainLabel := material.Body1(th, "+"+strconv.FormatInt(domainMax, 10))
					return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							call.Add(gtx.Ops)
							return dims
						}),
						layout.Rigid(func(gtx C) D {
							return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
								layout.Rigid(minDomainLabel.Layout),
								layout.Rigid(material.Body2(th, fmt.Sprintf("Unix Nanosecond Timestamp (%.3fs)", domainIntervalSecs)).Layout),
								layout.Rigid(maxDomainLabel.Layout),
							)
						}),
					)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return c.layoutKey(gtx, th)
		}),
	)
}

func (c *ChartData) layoutKey(gtx C, th *material.Theme) D {
	return outlay.FlowWrap{}.Layout(gtx, len(c.Headings), func(gtx layout.Context, i int) layout.Dimensions {
		return layout.UniformInset(8).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sideLen := gtx.Dp(10)
					sz := image.Pt(sideLen, sideLen)
					paint.FillShape(gtx.Ops, colors[i], clip.Rect{Max: sz}.Op())
					return D{Size: sz}
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					return material.Body2(th, c.Headings[i]).Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					return material.Body2(th, fmt.Sprintf("%.2f J", c.Sums[i])).Layout(gtx)
				}),
			)
		})
	})
}

func (c *ChartData) layoutPlot(gtx C) (D, int64, int64) {
	numSamples := gtx.Constraints.Max.X
	sampleStart := max(0, len(c.Samples)-numSamples)
	sampleEnd := min(len(c.Samples), numSamples+sampleStart)
	visibleSamples := c.Samples[sampleStart:sampleEnd]
	rangeInterval := float32(c.RangeMax - c.RangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}
	domainMin := visibleSamples[0].TimestampNS
	domainMax := visibleSamples[len(visibleSamples)-1].TimestampNS
	domainInterval := float32(domainMax - domainMin)
	if domainInterval == 0 {
		domainInterval = 1
	}
	for i := 0; i < len(c.Samples[0].Data); i++ {
		var p clip.Path
		p.Begin(gtx.Ops)
		for sampleIdx, sample := range visibleSamples {
			datum := sample.Data[i]
			x := (float32(sample.TimestampNS-domainMin) / domainInterval) * float32(gtx.Constraints.Max.X)
			y := float32(gtx.Constraints.Max.Y) - (float32(datum-c.RangeMin)/rangeInterval)*float32(gtx.Constraints.Max.Y)
			if sampleIdx == 0 {
				p.MoveTo(f32.Pt(x, y))
			} else {
				p.LineTo(f32.Pt(x, y))
			}
		}

		stack := clip.Stroke{
			Path:  p.End(),
			Width: float32(gtx.Dp(2)),
		}.Op().Push(gtx.Ops)
		paint.Fill(gtx.Ops, colors[i%len(colors)])
		stack.Pop()
	}
	return D{Size: gtx.Constraints.Max}, domainMin, domainMax
}

func loop(w *app.Window, headings []string, samples chan Sample) error {
	var data ChartData
	data.Headings = headings
	var ops op.Ops
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts())
	for {
		switch ev := w.NextEvent().(type) {
		case system.DestroyEvent:
			return ev.Err
		case system.FrameEvent:
			gtx := layout.NewContext(&ops, ev)
			select {
			case newData := <-samples:
				data.Insert(newData)
			default:
			}
			data.Layout(gtx, th)
			ev.Frame(gtx.Ops)
		}
	}
}
