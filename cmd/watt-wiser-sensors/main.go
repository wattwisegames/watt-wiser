package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"git.sr.ht/~whereswaldon/energy/hwmon"
	_ "git.sr.ht/~whereswaldon/energy/nvml"
	"git.sr.ht/~whereswaldon/energy/rapl"
	"git.sr.ht/~whereswaldon/energy/sensors"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `%[1]s: collect csv energy trace file from sensors
Usage:

 sudo %[1]s > file

OR

 sudo %[1]s | watt-wiser

Sadly, accessing RAPL requires root permissions, which is why this binary typically needs to run
as root.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
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
	samples := make([]float64, len(sensorList))
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
	ticker := time.NewTicker(sampleRate)
	defer ticker.Stop()
	for {
		select {
		case sampleEndTime := <-ticker.C:
			for chipIdx, chip := range sensorList {
				v, err := chip.Read()
				if err != nil {
					log.Fatalf("failed reading value: %v", err)
					return
				}
				samples[chipIdx] = v
			}
			readFinishedAt := time.Now()
			if readDuration := readFinishedAt.Sub(lastReadTime); readDuration < sampleRate*2 {
				// This sample was not interrupted mid-read, so we're good.
				fmt.Printf("%d, %d, ", lastReadTime.UnixNano(), sampleEndTime.UnixNano())
				sampleInterval := sampleEndTime.Sub(lastReadTime)
				for chipIdx, chip := range sensorList {
					v := samples[chipIdx]
					fmt.Printf("%f, ", v)
					if chip.Unit() == sensors.Watts {
						fmt.Printf("%f, ", v*sampleInterval.Seconds())
					}
				}
				fmt.Println()
			} else {
				log.Printf("dropping sample with read duration %d >= sample rate %d", readDuration, sampleRate)
			}
			lastReadTime = sampleEndTime
			for i := range samples {
				samples[i] = 0
			}
		}
	}
}
