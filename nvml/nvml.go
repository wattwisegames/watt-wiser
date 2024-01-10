//go:build linux

package nvml

/*
#cgo LDFLAGS: -ldl
#cgo pkg-config: nvidia-ml
#include <nvml.h>
#include <dlfcn.h>

typedef nvmlReturn_t (*nvmlInit_v2_type)();

nvmlReturn_t call_nvmlInit_v2(void *func) {
	return ((nvmlInit_v2_type) func)();
}

typedef nvmlReturn_t (*nvmlDeviceGetCount_v2_type)(unsigned int *deviceCount);

nvmlReturn_t call_nvmlDeviceGetCount_v2(void *func, unsigned int *deviceCount) {
	return ((nvmlDeviceGetCount_v2_type) func)(deviceCount);
}

typedef nvmlReturn_t (*nvmlDeviceGetHandleByIndex_v2_type)(unsigned int index, nvmlDevice_t *device);

nvmlReturn_t call_nvmlDeviceGetHandleByIndex_v2(void *func, unsigned int index, nvmlDevice_t *device) {
	return ((nvmlDeviceGetHandleByIndex_v2_type) func)(index, device);
}

typedef nvmlReturn_t (*nvmlDeviceGetTotalEnergyConsumption_type)(nvmlDevice_t device, unsigned long long *energy);

nvmlReturn_t call_nvmlDeviceGetTotalEnergyConsumption(void *func, nvmlDevice_t device, unsigned long long *energy) {
	return ((nvmlDeviceGetTotalEnergyConsumption_type) func)(device, energy);
}
*/
import "C"
import "log"

func init() {
	nvmlLib := C.dlopen(C.CString("libnvidia-ml.so"), C.RTLD_LAZY)
	if nvmlLib == nil {
		log.Printf("failed opening nvml library")
		return
	}
	initV2 := C.dlsym(nvmlLib, C.CString("nvmlInit_v2"))
	if initV2 == nil {
		log.Printf("failed resolving nvml init func")
		return
	}
	countV2 := C.dlsym(nvmlLib, C.CString("nvmlDeviceGetCount_v2"))
	if countV2 == nil {
		log.Printf("failed resolving nvml count func")
		return
	}
	getHandleV2 := C.dlsym(nvmlLib, C.CString("nvmlDeviceGetHandleByIndex_v2"))
	if countV2 == nil {
		log.Printf("failed resolving nvml get device handle func")
		return
	}
	getEnergy := C.dlsym(nvmlLib, C.CString("nvmlDeviceGetTotalEnergyConsumption"))
	if countV2 == nil {
		log.Printf("failed resolving nvml get energy func")
		return
	}
	rc := C.call_nvmlInit_v2(initV2)
	if rc != C.NVML_SUCCESS {
		log.Printf("failed initializing nvml")
		return
	}
	var count C.unsigned
	rc = C.call_nvmlDeviceGetCount_v2(countV2, &count)
	if rc != C.NVML_SUCCESS {
		log.Printf("failed counting devices")
		return
	}
	log.Printf("detected %d gpu devices", count)
	for i := C.unsigned(0); i < count; i++ {
		var device C.nvmlDevice_t
		rc = C.call_nvmlDeviceGetHandleByIndex_v2(getHandleV2, i, &device)
		if rc != C.NVML_SUCCESS {
			log.Printf("failed acquiring device handle")
			return
		}
		var energy C.ulonglong
		rc = C.call_nvmlDeviceGetTotalEnergyConsumption(getEnergy, device, &energy)
		if rc != C.NVML_SUCCESS {
			log.Printf("failed reading used energy")
			return
		}
		log.Printf("Read %d uJ", energy)
	}
}
