//go:build windows && cgo

package adlx

/*
#include "ADLXHelper.h"
#include "include/IPerformanceMonitoring.h"

ADLX_RESULT sys_get_performance_monitoring_services(IADLXSystem *sys, IADLXPerformanceMonitoringServices **perfMonitoringService) {
	return sys->pVtbl->GetPerformanceMonitoringServices(sys, perfMonitoringService);
}
ADLX_RESULT sys_get_gpus(IADLXSystem *sys, IADLXGPUList** gpus) {
    return sys->pVtbl->GetGPUs(sys, gpus);
}
ADLX_RESULT perf_get_metrics_support(IADLXPerformanceMonitoringServices *perf, IADLXGPU *gpu, IADLXGPUMetricsSupport **metricsSupport) {
	return perf->pVtbl->GetSupportedGPUMetrics(perf, gpu, metricsSupport);
}
ADLX_RESULT metrics_support_power(IADLXGPUMetricsSupport *metricsSupport, adlx_bool *supported) {
	return metricsSupport->pVtbl->IsSupportedGPUPower(metricsSupport, supported);
}
ADLX_RESULT metrics_support_total_board_power(IADLXGPUMetricsSupport *metricsSupport, adlx_bool *supported) {
	return metricsSupport->pVtbl->IsSupportedGPUTotalBoardPower(metricsSupport, supported);
}
ADLX_RESULT perf_get_metrics(IADLXPerformanceMonitoringServices *perf, IADLXGPU *gpu, IADLXGPUMetrics **metrics) {
	return perf->pVtbl->GetCurrentGPUMetrics(perf, gpu, metrics);
}
ADLX_RESULT metrics_gpu_power(IADLXGPUMetrics *metrics, adlx_double *power) {
    return metrics->pVtbl->GPUPower(metrics, power);
}
ADLX_RESULT metrics_gpu_total_board_power(IADLXGPUMetrics *metrics, adlx_double *power) {
    return metrics->pVtbl->GPUTotalBoardPower(metrics, power);
}
void perf_release(IADLXPerformanceMonitoringServices *perf) {
	perf->pVtbl->Release(perf);
}
ADLX_RESULT gpus_at_gpu_list(IADLXGPUList *gpus, int index, IADLXGPU **gpu) {
    return gpus->pVtbl->At_GPUList(gpus, index, gpu);
}
int gpus_begin(IADLXGPUList *gpus) {
    return gpus->pVtbl->Begin(gpus);
}
void gpus_release(IADLXGPUList *gpus) {
    gpus->pVtbl->Release(gpus);
}
void gpu_release(IADLXGPU *gpu) {
    gpu->pVtbl->Release(gpu);
}
*/
import "C"
import (
	"fmt"

	"git.sr.ht/~whereswaldon/watt-wiser/sensors"
)

type sensor struct {
	metrics    *C.IADLXGPUMetrics
	name       string
	totalBoard bool
}

var _ sensors.Sensor = sensor{}

func (s sensor) Unit() sensors.Unit {
	return sensors.Watts
}

func (s sensor) Name() string {
	if s.totalBoard {
		return s.name + " Total Board Power"
	} else {
		return s.name + " Power"
	}
}

func (s sensor) Read() (float64, error) {
	var power C.adlx_double
	var res C.ADLX_RESULT
	if s.totalBoard {
		res = C.metrics_gpu_total_board_power(s.metrics, &power)
	} else {
		res = C.metrics_gpu_power(s.metrics, &power)
	}
	if res != 0 {
		return 0, fmt.Errorf("failed reading %q: %d", s.Name(), res)
	}
	return float64(power), nil
}

func FindSensors() ([]sensors.Sensor, error) {
	sensorList := []sensors.Sensor{}
	var res C.ADLX_RESULT
	// Initialize ADLX
	res = C.ADLXHelper_Initialize()
	if res != 0 {
		return nil, fmt.Errorf("failed init: %d", res)
	}
	// Get Performance Monitoring services
	sys := C.ADLXHelper_GetSystemServices()
	var perfMonitoringService *C.IADLXPerformanceMonitoringServices
	res = C.sys_get_performance_monitoring_services(sys, &perfMonitoringService)
	if res != 0 {
		return nil, fmt.Errorf("failed getting performance service: %d", res)
	}
	defer C.perf_release(perfMonitoringService)
	var gpus *C.IADLXGPUList
	res = C.sys_get_gpus(sys, &gpus)
	if res != 0 {
		return nil, fmt.Errorf("failed getting gpus: %d", res)
	}
	defer C.gpus_release(gpus)
	var firstGPU *C.IADLXGPU
	res = C.gpus_at_gpu_list(gpus, C.gpus_begin(gpus), &firstGPU)
	if res != 0 {
		return nil, fmt.Errorf("failed getting first gpu: %d", res)
	}
	defer C.gpu_release(firstGPU)
	// Get GPU metrics support
	var metricsSupport *C.IADLXGPUMetricsSupport
	res = C.perf_get_metrics_support(perfMonitoringService, firstGPU, &metricsSupport)
	if res != 0 {
		return nil, fmt.Errorf("failed getting first gpu metrics support: %d", res)
	}
	var metrics *C.IADLXGPUMetrics
	res = C.perf_get_metrics(perfMonitoringService, firstGPU, &metrics)
	if res != 0 {
		return nil, fmt.Errorf("failed getting first gpu metrics: %d", res)
	}
	var supportsPower C.adlx_bool
	res = C.metrics_support_power(metricsSupport, &supportsPower)
	if res != 0 {
		return nil, fmt.Errorf("failed getting first gpu power support: %d", res)
	}
	if supportsPower != 0 {
		sensorList = append(sensorList, sensor{
			name:    "AMD GPU",
			metrics: metrics,
		})
	}
	var supportsTotalBoardPower C.adlx_bool
	res = C.metrics_support_total_board_power(metricsSupport, &supportsTotalBoardPower)
	if res != 0 {
		return nil, fmt.Errorf("failed getting first gpu total board power support: %d", res)
	}
	if supportsTotalBoardPower != 0 {
		sensorList = append(sensorList, sensor{
			name:       "AMD GPU",
			metrics:    metrics,
			totalBoard: true,
		})
	}
	return sensorList, nil
}
