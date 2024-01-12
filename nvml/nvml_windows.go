//go:build windows

package nvml

import (
	"fmt"
	"log"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	nvmlSuccess uintptr = 0
)

func findNVMLDLL() (*windows.LazyDLL, error) {
	for _, path := range []string{
		os.ExpandEnv("${ProgramW6432}\\NVIDIA Corporation\\NVSMI\\nvml.dll"),
		"nvml.dll",
	} {
		log.Printf("Trying to load %s", path)
		tryNVML := windows.NewLazySystemDLL(path)
		if err := tryNVML.Load(); err == nil {
			log.Printf("Loaded %s", path)
			return tryNVML, nil
		}
	}
	return nil, fmt.Errorf("failed to find nvml.dll")
}

func init() {
	nvml, err := findNVMLDLL()
	if err != nil {
		log.Printf("%v", err)
		return
	}

	initNVML := nvml.NewProc("nvmlInit_v2")
	countDevices := nvml.NewProc("nvmlDeviceGetCount_v2")
	getDeviceHandleByIndex := nvml.NewProc("nvmlDeviceGetHandleByIndex_v2")
	getEnergy := nvml.NewProc("nvmlDeviceGetTotalEnergyConsumption")
	getPower := nvml.NewProc("nvmlDeviceGetPowerUsage")
	getArchitecture := nvml.NewProc("nvmlDeviceGetArchitecture")
	getNVMLVersion := nvml.NewProc("nvmlSystemGetNVMLVersion")

	var version [16]byte
	r1, _, err := getNVMLVersion.Call(uintptr(unsafe.Pointer(&version)), 16)
	if r1 != nvmlSuccess {
		log.Printf("failed to get NVML version: %v", err)
		return
	}
	log.Printf("NVML version: %s", string(version[:]))

	useEnergy := false

	err = getEnergy.Find()
	if err != nil {
		log.Printf("energy monitoring unavailable")
	} else {
		useEnergy = true
	}
	err = getPower.Find()
	if err != nil {
		log.Printf("power monitoring unavailable")
		if !useEnergy {
			log.Printf("neither energy nor power monitoring available, giving up")
			return
		}
	}
	r1, _, err = initNVML.Call()
	if r1 != nvmlSuccess {
		log.Printf("failed initializing: %v", err)
		return
	}
	var deviceCount uint64
	r1, _, err = countDevices.Call(uintptr(unsafe.Pointer(&deviceCount)))
	if r1 != nvmlSuccess {
		log.Printf("failed counting devices: %v", err)
		return
	}
	log.Printf("found %d devices", deviceCount)
	for i := uint64(0); i < deviceCount; i++ {
		var device uintptr
		r1, _, err = getDeviceHandleByIndex.Call(uintptr(i), uintptr(unsafe.Pointer(&device)))
		if r1 != nvmlSuccess {
			log.Printf("failed counting devices: %v", err)
			return
		}
		log.Printf("Loaded device %v", device)
		var arch uint32
		r1, _, err = getArchitecture.Call(device, uintptr(unsafe.Pointer(&arch)))
		if r1 != nvmlSuccess {
			log.Printf("failed getting device architecture: %v", err)
			return
		}
		log.Printf("Device architecture: %v", arch)
		if useEnergy {
			var uJ uint64
			r1, _, err = getEnergy.Call(device, uintptr(unsafe.Pointer(&uJ)))
			if r1 != nvmlSuccess {
				log.Printf("failed reading energy: %v", err)
				return
			}
			log.Printf("Read %d uJ", uJ)
		} else {
			var mW uint32
			r1, _, err = getPower.Call(device, uintptr(unsafe.Pointer(&mW)))
			if r1 != nvmlSuccess {
				log.Printf("failed reading power: %v", err)
			}
			log.Printf("Read %d mW", mW)
		}
	}
}
