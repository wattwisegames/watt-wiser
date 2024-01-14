//go:build windows

package nvml

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func findNVMLDLL() (*windows.LazyDLL, error) {
	for _, path := range []string{
		os.ExpandEnv("${ProgramW6432}\\NVIDIA Corporation\\NVSMI\\nvml.dll"),
		"nvml.dll",
	} {
		tryNVML := windows.NewLazySystemDLL(path)
		if err := tryNVML.Load(); err == nil {
			return tryNVML, nil
		}
	}
	return nil, fmt.Errorf("failed to find nvml.dll")
}

func platformInit() error {
	nvml, err := findNVMLDLL()
	if err != nil {
		return err
	}

	resolved := map[string]*windows.LazyProc{}

	for _, symbol := range requiredSymbols {
		lazySym := nvml.NewProc(symbol)
		if err := lazySym.Find(); err != nil {
			return fmt.Errorf("failed resolving required symbol %q: %w", symbol, err)
		}
		resolved[symbol] = lazySym
	}
	for _, symbol := range optionalSymbols {
		lazySym := nvml.NewProc(symbol)
		if err := lazySym.Find(); err != nil {
			continue
		}
		resolved[symbol] = lazySym
	}

	// Build wrappers for required symbols
	initFunc := resolved[symbolNvmlInit_v2]
	nvmlInit = func() error {
		rc, _, _ := initFunc.Call()
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return rc
		}
		return nil
	}
	getVersionFunc := resolved[symbolNvmlSystemGetNVMLVersion]
	nvmlSystemGetNVMLVersion = func() (string, error) {
		var version [16]byte
		rc, _, _ := getVersionFunc.Call(uintptr(unsafe.Pointer(&version)), uintptr(len(version)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return "", rc
		}
		return strings.ReplaceAll(string(version[:]), "\000", ""), nil
	}
	getCountFunc := resolved[symbolNvmlDeviceGetCount_v2]
	nvmlDeviceGetCount = func() (uint64, error) {
		var count uint64
		rc, _, _ := getCountFunc.Call(uintptr(unsafe.Pointer(&count)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return 0, rc
		}
		return count, nil

	}
	getHandleFunc := resolved[symbolNvmlDeviceGetHandleByIndex_v2]
	nvmlDeviceGetHandleByIndex = func(i uint64) (uintptr, error) {
		var device uintptr
		rc, _, _ := getHandleFunc.Call(uintptr(i), uintptr(unsafe.Pointer(&device)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return 0, rc
		}
		return device, nil
	}
	getNameFunc := resolved[symbolNvmlDeviceGetName]
	nvmlDeviceGetName = func(device uintptr) (string, error) {
		var buf [96]byte
		rc, _, _ := getNameFunc.Call(device, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return "", rc
		}
		return strings.ReplaceAll(string(buf[:]), "\000", ""), nil
	}
	getPowerFunc := resolved[symbolNvmlDeviceGetPowerUsage]
	nvmlDeviceGetPowerUsage = func(device uintptr) (uint32, error) {
		var mW uint32
		rc, _, _ := getPowerFunc.Call(device, uintptr(unsafe.Pointer(&mW)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return 0, rc
		}
		return mW, nil
	}
	// Optional symbols
	if getArchFunc, ok := resolved[symbolNvmlDeviceGetArchitecture]; ok {
		nvmlDeviceGetArchitecture = func(device uintptr) (nvmlDeviceArchitecture, error) {
			var arch nvmlDeviceArchitecture
			rc, _, _ := getArchFunc.Call(device, uintptr(unsafe.Pointer(&arch)))
			if rc := nvmlError(rc); rc != NVML_SUCCESS {
				return 0, rc
			}
			return arch, nil
		}
	}
	if getEnergyFunc, ok := resolved[symbolNvmlDeviceGetTotalEnergyConsumption]; ok {
		nvmlDeviceGetTotalEnergyConsumption = func(device uintptr) (uint64, error) {
			var uJ uint64
			rc, _, _ := getEnergyFunc.Call(device, uintptr(unsafe.Pointer(&uJ)))
			if rc := nvmlError(rc); rc != NVML_SUCCESS {
				return 0, rc
			}
			return uJ, nil
		}
	}
	return nil
}
