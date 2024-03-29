package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"time"

	"git.sr.ht/~whereswaldon/watt-wiser/adlx"
	"git.sr.ht/~whereswaldon/watt-wiser/hwmon"
	"git.sr.ht/~whereswaldon/watt-wiser/nvml"
	"git.sr.ht/~whereswaldon/watt-wiser/rapl"
	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
)

func linuxUsage() {
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

func windowsUsage() {
	fmt.Fprintf(flag.CommandLine.Output(), `%[1]s: collect csv energy trace file from sensors
Usage:

 %[1]s > file

OR

 %[1]s | watt-wiser

`, os.Args[0])
	flag.PrintDefaults()
}

func unsupportedUsage() {
	fmt.Fprintf(flag.CommandLine.Output(), `%[1]s: collect csv energy trace file from sensors

This platform is unsupported; no sensor data is available.

`, os.Args[0])
	flag.PrintDefaults()
}

func main() {
	switch runtime.GOOS {
	case "linux":
		flag.Usage = linuxUsage
	case "windows":
		flag.Usage = windowsUsage
	default:
		flag.Usage = unsupportedUsage
	}
	dur := flag.Duration("sample-interval", 100*time.Millisecond, "Interval between reading new samples from sensors")
	outputName := flag.String("output", "-", "Output file for CSV sensor data")
	flag.Parse()
	raplSensors, err := rapl.FindRAPL()
	if err != nil {
		log.Printf("failed loading RAPL sensors: %v", err)
	}
	hwmonSensors, err := hwmon.FindEnergySensors()
	if err != nil {
		log.Printf("failed loading HWMON sensors: %v", err)
	}
	nvidiaGPUSensors, err := nvml.FindGPUSensors()
	if err != nil {
		log.Printf("failed loading NVIDIA GPU sensors: %v", err)
	}
	amdGPUSensors, err := adlx.FindSensors()
	if err != nil {
		log.Printf("failed loading AMD GPU sensors: %v", err)
	}

	var output io.WriteCloser
	if *outputName == "-" {
		output = os.Stdout
	} else {
		f, err := os.Create(*outputName)
		if err != nil {
			log.Fatalf("failed opening output file %q: %v", *outputName, err)
		}
		output = f
	}
	sensorList := make([]sensors.Sensor, 0, len(raplSensors)+len(hwmonSensors)+len(nvidiaGPUSensors))
	sensorList = append(sensorList, raplSensors...)
	sensorList = append(sensorList, hwmonSensors...)
	sensorList = append(sensorList, nvidiaGPUSensors...)
	sensorList = append(sensorList, amdGPUSensors...)

	if len(sensorList) < 1 {
		log.Fatalf("No supported sensors found. Please see https://git.sr.ht/~whereswaldon/watt-wiser or https://github.com/wattwisegames/watt-wiser for supported hardware information")
	}

	fmt.Fprintf(output, "sample start (ns), sample end (ns), ")
	for _, s := range sensorList {
		fmt.Fprintf(output, "%s (%s), ", s.Name(), s.Unit())
	}
	fmt.Fprintln(output)
	samples := make([]float64, len(sensorList))
	absStartTime := time.Now()
	lastReadTime := absStartTime.UnixNano()
	// Pre-read every sensor once to ensure that incremental sensors emit coherent first values.
	for _, chip := range sensorList {
		_, err := chip.Read()
		if err != nil {
			log.Fatalf("failed reading value: %v", err)
			return
		}
	}
	sampleRate := *dur
	ticker := time.NewTicker(sampleRate)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	defer ticker.Stop()
	for {
		select {
		case <-sigChan:
			// We've gotten an interrupt; shut down.
			if err := output.Close(); err != nil {
				log.Printf("failed closing output: %v", err)
			}
			return
		case sampleEndTime := <-ticker.C:
			for chipIdx, chip := range sensorList {
				v, err := chip.Read()
				if err != nil {
					log.Fatalf("failed reading value: %v", err)
					return
				}
				samples[chipIdx] = v
			}
			// Compute durations spent reading sensors relative to our fixed start time.
			readFinishedAt := time.Since(absStartTime)
			readStartedAt := sampleEndTime.Sub(absStartTime)
			// Then compute the end of the sample by adding a monotonic interval to our fixed
			// start time to avoid clock skew.
			readStartAbs := absStartTime.UnixNano() + readStartedAt.Nanoseconds()
			if readDuration := readFinishedAt - readStartedAt; readDuration < sampleRate*2 {
				// This sample was not interrupted mid-read, so we're good.
				fmt.Fprintf(output, "%d, %d, ", lastReadTime, readStartAbs)
				for chipIdx := range sensorList {
					v := samples[chipIdx]
					fmt.Fprintf(output, "%f, ", v)
				}
				fmt.Fprintln(output)
			} else {
				log.Printf("dropping sample with read duration %d >= sample rate %d", readDuration, sampleRate)
			}
			// Update our lastReadTime to the non-clock-skewed timestamp of when we started the sensor reads.
			lastReadTime = readStartAbs
			for i := range samples {
				samples[i] = 0
			}
		}
	}
}
