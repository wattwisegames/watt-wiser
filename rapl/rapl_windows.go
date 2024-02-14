//go:build windows

/*
This file is adapted from code available here:
https://github.com/hubblo-org/scaphandre/blob/5525c68b5a96bbbba39bce6ae65d6a4ecdb1dab2/src/sensors/msr_rapl.rs#L15

As such, it is available under the terms of the original license (Apache 2.0):
https://github.com/hubblo-org/scaphandre/blob/5525c68b5a96bbbba39bce6ae65d6a4ecdb1dab2/LICENSE
*/

package rapl

import (
	"fmt"
	"math"
	"unsafe"

	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
	"golang.org/x/sys/windows"
)

type manufacturer uint8
const (
	Intel manufacturer = iota
	AMD
)

const (
	// Intel MSRs
	MSR_RAPL_POWER_UNIT   uint64= 0x606
	MSR_PKG_ENERGY_STATUS uint64= 0x611

	// AMD MSRs
	MSR_AMD_RAPL_POWER_UNIT uint64 = 0xc0010299
	MSR_AMD_PKG_ENERGY_STATUS uint64 = 0xc001029b
)

// These constants borrowed from:
// https://docs.rs/windows-sys/latest/windows_sys/Win32/System/Ioctl/
const (
	FILE_DEVICE_UNKNOWN uint32 = 34
	METHOD_BUFFERED     uint32 = 0
)

type RAPLSensor struct {
	driverName string
	manufacturer
	powerUnit  float64
	energyUnit float64
	timeUnit   float64
	handle     windows.Handle
	previous   float64
}

func getHandle(driverName string) (windows.Handle, error) {
	name, err := windows.UTF16FromString(driverName)
	if err != nil {
		return 0, fmt.Errorf("failed converting driver name string: %w", err)
	}
	return windows.CreateFile(
		unsafe.SliceData(name),
		windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
}

func controlCode(deviceType, requestCode, method, access uint32) uint32 {
	return (deviceType << 16) | (access << 14) | (requestCode << 2) | method
}

func sendRequest(
	device windows.Handle,
	requestData uint64,
) (uint64, error) {
	requestCode := uint16(MSR_RAPL_POWER_UNIT)
	var replyUsed uint32
	request := unsafe.Slice((*byte)(unsafe.Pointer(&requestData)), unsafe.Sizeof(requestData))
	var responseData uint64
	response := unsafe.Slice((*byte)(unsafe.Pointer(&responseData)), unsafe.Sizeof(responseData))
	if err := windows.DeviceIoControl(
		device,
		controlCode(
			FILE_DEVICE_UNKNOWN,
			uint32(requestCode),
			METHOD_BUFFERED,
			windows.FILE_READ_DATA|windows.FILE_WRITE_DATA,
		),
		unsafe.SliceData(request),
		uint32(len(request)),
		unsafe.SliceData(response),
		uint32(len(response)),
		&replyUsed,
		nil,
	); err != nil {
		return 0, fmt.Errorf("failed sending request: %w", err)
	} else if replyUsed != uint32(len(response)) {
		return 0, fmt.Errorf("expected %d bytes in response, got %d", len(response), replyUsed)
	}
	return responseData, nil
}

func extractRAPLPowerUnit(data uint64) float64 {
	// Discard higher bits that are reserved for future use.
	data &= 0xffffffff
	// Power is in bits 0-3
	power := data & 0x0f
	denom := math.Pow(2, float64(power))
	return 1 / denom
}

func extractRAPLEnergyUnit(data uint64) float64 {
	// Discard higher bits that are reserved for future use.
	data &= 0xffffffff
	// Energy is in bits 8-12
	energy := (data >> 8) & 0x1f
	denom := math.Pow(2, float64(energy))
	return 1 / denom
}

func extractRAPLTimeUnit(data uint64) float64 {
	// Discard higher bits that are reserved for future use.
	data &= 0xffffffff
	// Time is in bits 16-19
	time := (data >> 16) & 0x0f
	denom := math.Pow(2, float64(time))
	return 1 / denom
}

func extractEnergyData(data uint64, unit float64) float64 {
	data &= 0xffffffff
	return float64(data) * unit
}

func FindRAPL() ([]sensors.Sensor, error) {
	s, err := NewRAPLSensor()
	if err != nil {
		return nil, err
	}
	return []sensors.Sensor{s}, err
}

func NewRAPLSensor() (*RAPLSensor, error) {
	sensor := &RAPLSensor{
		driverName: `\\.\ScaphandreDriver`,
	}
	handle, err := getHandle(sensor.driverName)
	if err != nil {
		return nil, fmt.Errorf("failed acquiring sensor file handle: %w", err)
	}
	sensor.manufacturer = Intel
	response, err := sendRequest(
		handle,
		MSR_RAPL_POWER_UNIT,
	)
	if err != nil {
		origErr := err
		// Might be an AMD system, try that.
		response, err = sendRequest(
			handle,
			MSR_AMD_RAPL_POWER_UNIT,
		)
		if err != nil {
			return nil, fmt.Errorf("failed communicating with rapl driver: %w %w", origErr, err)
		}
		sensor.manufacturer = AMD
	}
	sensor.powerUnit = extractRAPLPowerUnit(response)
	sensor.energyUnit = extractRAPLEnergyUnit(response)
	sensor.timeUnit = extractRAPLTimeUnit(response)
	sensor.handle = handle
	return sensor, nil
}

func (r *RAPLSensor) energyMSR() uint64 {
	switch r.manufacturer {
	case Intel:
		return MSR_PKG_ENERGY_STATUS
	case AMD:
		return MSR_AMD_PKG_ENERGY_STATUS
	default:
		return 0
	}
}

func (r *RAPLSensor) Read() (float64, error) {
	response, err := sendRequest(
		r.handle,
		r.energyMSR(),
	)
	if err != nil {
		return 0, fmt.Errorf("failed reading energy: %w", err)
	}
	raw := extractEnergyData(response, r.energyUnit)
	inc := raw - r.previous
	r.previous = raw
	return inc, nil
}

func (r *RAPLSensor) Name() string {
	return "package-0"
}

func (r *RAPLSensor) Unit() sensors.Unit {
	return sensors.Joules
}
