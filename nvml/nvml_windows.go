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
	nvmlInit = func() error {
		rc, _, _ := resolved[symbolNvmlInit_v2].Call()
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return rc
		}
		return nil
	}
	nvmlSystemGetNVMLVersion = func() (string, error) {
		var version [16]byte
		rc, _, _ := resolved[symbolNvmlSystemGetNVMLVersion].Call(uintptr(unsafe.Pointer(&version)), uintptr(len(version)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return "", rc
		}
		return strings.ReplaceAll(string(version[:]), "\000", ""), nil
	}
	nvmlDeviceGetCount = func() (uint64, error) {
		var count uint64
		rc, _, _ := resolved[symbolNvmlDeviceGetCount_v2].Call(uintptr(unsafe.Pointer(&count)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return 0, rc
		}
		return count, nil

	}
	nvmlDeviceGetHandleByIndex = func(i uint64) (uintptr, error) {
		var device uintptr
		rc, _, _ := resolved[symbolNvmlDeviceGetHandleByIndex_v2].Call(uintptr(i), uintptr(unsafe.Pointer(&device)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return 0, rc
		}
		return device, nil
	}
	nvmlDeviceGetName = func(device uintptr) (string, error) {
		var buf [96]byte 
		rc, _, _ := resolved[symbolNvmlDeviceGetName].Call(device, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return "", rc
		}
		return strings.ReplaceAll(string(buf[:]), "\000", ""), nil
	}
	nvmlDeviceGetPowerUsage = func(device uintptr) (uint32, error) {
		var mW uint32
		rc, _, _ := resolved[symbolNvmlDeviceGetPowerUsage].Call(device, uintptr(unsafe.Pointer(&mW)))
		if rc := nvmlError(rc); rc != NVML_SUCCESS {
			return 0, rc
		}
		return mW, nil
	}
	// Optional symbols
	if f, ok := resolved[symbolNvmlDeviceGetArchitecture]; ok {
		nvmlDeviceGetArchitecture = func(device uintptr) (nvmlDeviceArchitecture, error) {
			var arch nvmlDeviceArchitecture
			rc, _, _ := f.Call(device, uintptr(unsafe.Pointer(&arch)))
			if rc := nvmlError(rc); rc != NVML_SUCCESS {
				return 0, rc
			}
			return arch, nil
		}
	}
	if f, ok := resolved[symbolNvmlDeviceGetTotalEnergyConsumption]; ok {
		nvmlDeviceGetTotalEnergyConsumption = func(device uintptr) (uint64, error) {
			var uJ uint64
			rc, _, _ := f.Call(device, uintptr(unsafe.Pointer(&uJ)))
			if rc := nvmlError(rc); rc != NVML_SUCCESS {
				return 0, rc
			}
			return uJ, nil
		}
	}
	return nil
}
