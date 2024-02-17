//go:build !windows || !cgo

package adlx

import (
	"fmt"

	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
)

func FindSensors() ([]sensors.Sensor, error) {
	return nil, fmt.Errorf("ADLX not available on this platform")
}
