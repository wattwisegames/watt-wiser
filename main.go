package main

import (
	"fmt"
	"log"
	"time"

	"git.sr.ht/~whereswaldon/energy/hwmon"
	"git.sr.ht/~whereswaldon/energy/rapl"
	"git.sr.ht/~whereswaldon/energy/sensors"
)

func main() {
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
