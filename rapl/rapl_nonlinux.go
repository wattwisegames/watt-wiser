//go:build !linux
package rapl

import "git.sr.ht/~whereswaldon/watt-wiser/sensors"

func FindRAPL() ([]sensors.Sensor, error) {
	return nil, nil
}
