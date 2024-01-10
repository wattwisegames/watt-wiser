//go:build !linux
package rapl

import "git.sr.ht/~whereswaldon/energy/sensors"

func FindRAPL() ([]sensors.Sensor, error) {
	return nil, nil
}
