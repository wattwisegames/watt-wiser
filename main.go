package main

import (
	"fmt"
	"log"
	"time"
)

type Unit uint8

func (u Unit) String() string {
	switch u {
	case Joules:
		return "J"
	case Watts:
		return "W"
	case Amps:
		return "A"
	case Volts:
		return "V"
	default:
		return "?"
	}
}

const (
	Joules Unit = iota
	Watts
	Amps
	Volts
	Unknown
)

const (
	// microToUnprefixed is the conversion factor from a micro SI unit to an unprefixed
	// one.
	microToUnprefixed = 1.0 / 1_000_000
)

type Sensor interface {
	Name() string
	Unit() Unit
	Read() (float64, error)
}

func main() {
	raplWatches, err := FindRAPL()
	if err != nil {
		log.Fatal(err)
	}
	relevantSubfeatures, err := FindSubfeatures()
	if err != nil {
		log.Fatal(err)
	}
	sensors := make([]Sensor, 0, len(raplWatches)+len(relevantSubfeatures))
	for _, w := range raplWatches {
		sensors = append(sensors, w)
	}
	for _, s := range relevantSubfeatures {
		sensors = append(sensors, s)
	}
	fmt.Printf("timestamp_ns, ")
	for _, s := range sensors {
		fmt.Printf("%s (%s), ", s.Name(), s.Unit())
		if s.Unit() == Watts {
			fmt.Printf("integrated %s (%s),", s.Name(), Joules)
		}
	}
	fmt.Println()
	// Pre-read every sensor once to ensure that incremental sensors emit coherent first values.
	for _, chip := range sensors {
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
			for _, chip := range sensors {
				v, err := chip.Read()
				if err != nil {
					log.Fatalf("failed reading value: %v", err)
					return
				}
				fmt.Printf("%f, ", v)
				if chip.Unit() == Watts {
					fmt.Printf("%f, ", v*sampleRateSeconds)
				}
			}
			fmt.Println()
		}
	}
}
