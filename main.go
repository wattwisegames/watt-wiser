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
		for i, heading := range headings {
			if i == 0 {
				continue
			}
			if strings.Contains(heading, "(J)") {
				relevantIndices = append(relevantIndices, i)
			}
		}

		w := app.NewWindow()
		go func() {
			if err := loop(w, headings, samplesChan); err != nil {
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
}

func (c *ChartData) Insert(sample Sample) {
	if len(c.Samples) == 0 {
		c.DomainMin = sample.TimestampNS
		c.DomainMax = sample.TimestampNS
		c.RangeMin = sample.Data[0]
		c.RangeMax = sample.Data[0]
	}
	for _, datum := range sample.Data {
		c.RangeMin = min(datum, c.RangeMin)
		c.RangeMax = max(datum, c.RangeMax)
	}
	c.DomainMin = min(sample.TimestampNS, c.DomainMin)
	c.DomainMax = max(sample.TimestampNS, c.DomainMax)
	c.Samples = append(c.Samples, sample)
}

var colors = []color.NRGBA{
	{R: 255, A: 100},
	{G: 255, A: 100},
	{B: 255, A: 100},
	{R: 255, G: 255, A: 100},
	{R: 255, B: 255, A: 100},
	{G: 255, B: 255, A: 100},
}

func (c *ChartData) Layout(gtx C, th *material.Theme) D {
	if len(c.Samples) < 1 {
		return D{Size: gtx.Constraints.Max}
	}
	rangeInterval := float32(c.RangeMax - c.RangeMin)
	if rangeInterval == 0 {
		rangeInterval = 1
	}
	domainInterval := float32(c.DomainMax - c.DomainMin)
	if domainInterval == 0 {
		domainInterval = 1
	}
	fmt.Println("intervals", domainInterval, rangeInterval)

	minRangeLabel := material.Body1(th, strconv.FormatFloat(c.RangeMin, 'f', 3, 64))
	maxRangeLabel := material.Body1(th, strconv.FormatFloat(c.RangeMax, 'f', 3, 64))
	minDomainLabel := material.Body1(th, strconv.FormatInt(c.DomainMin, 10))
	maxDomainLabel := material.Body1(th, strconv.FormatInt(c.DomainMax, 10))
	origConstraints := gtx.Constraints
	gtx.Constraints.Min = image.Point{}
	macro := op.Record(gtx.Ops)
	domainDims := minDomainLabel.Layout(gtx)
	_ = macro.Stop()
	gtx.Constraints = origConstraints
	return layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Rigid(maxRangeLabel.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return D{Size: gtx.Constraints.Min}
				}),
				layout.Rigid(minRangeLabel.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return D{Size: image.Point{
						Y: domainDims.Size.Y,
					}}
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					for i := 0; i < len(c.Samples[0].Data); i++ {
						var p clip.Path
						p.Begin(gtx.Ops)
						for _, sample := range c.Samples {
							datum := sample.Data[i]
							x := (float32(sample.TimestampNS-c.DomainMin) / domainInterval) * float32(gtx.Constraints.Max.X)
							y := (float32(datum-c.RangeMin) / rangeInterval) * float32(gtx.Constraints.Max.Y)
							p.LineTo(f32.Pt(x, y))
							log.Println(x, y)
						}
						p.LineTo(f32.Pt(float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y)))
						p.LineTo(f32.Pt(0, float32(gtx.Constraints.Max.Y)))
						p.LineTo(f32.Pt(0, 0))
						p.Close()

						stack := clip.Outline{
							Path: p.End(),
						}.Op().Push(gtx.Ops)
						paint.Fill(gtx.Ops, colors[i%len(colors)])
						stack.Pop()
					}
					return D{Size: gtx.Constraints.Max}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(minDomainLabel.Layout),
						layout.Rigid(maxDomainLabel.Layout),
					)
				}),
			)
		}),
	)

}

func loop(w *app.Window, headings []string, samples chan Sample) error {
	var data ChartData
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
