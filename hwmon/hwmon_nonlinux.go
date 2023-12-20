//go:build !linux

package hwmon

func FindEnergySensors() ([]sensors.Sensor, error) {
	return nil, nil
}
