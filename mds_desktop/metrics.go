package main

type metricsSnapshot struct {
	CPUPercent       float64           `json:"cpu_percent,omitempty"`
	MemoryPercent    float64           `json:"memory_percent,omitempty"`
	MemoryUsedBytes  uint64            `json:"memory_used_bytes,omitempty"`
	MemoryTotalBytes uint64            `json:"memory_total_bytes,omitempty"`
	DiskUsedPercent  float64           `json:"disk_used_percent,omitempty"`
	DiskFreeBytes    uint64            `json:"disk_free_bytes,omitempty"`
	DiskTotalBytes   uint64            `json:"disk_total_bytes,omitempty"`
	ForegroundApp    *appSnapshot      `json:"-"`
	Processes        []processSnapshot `json:"-"`
	CollectionErrors []string          `json:"collection_errors,omitempty"`
}

func minUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}
