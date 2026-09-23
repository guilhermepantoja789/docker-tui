package metrics

import (
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/guilhermepantoja789/docker-tui/internal/model"
)

// Sample is a raw stats observation used to compute deltas.
type Sample struct {
	CPUTotal   uint64
	SystemCPU  uint64
	OnlineCPUs uint32
	NumProcs   uint32
	MemUsage   uint64
	MemLimit   uint64
	NetRx      uint64
	NetTx      uint64
	BlkRead    uint64
	BlkWrite   uint64
	At         time.Time
}

// FromStatsResponse extracts counter fields from a Docker stats payload.
func FromStatsResponse(s *container.StatsResponse, at time.Time) Sample {
	var netRx, netTx uint64
	for _, n := range s.Networks {
		netRx += n.RxBytes
		netTx += n.TxBytes
	}

	var blkRead, blkWrite uint64
	for _, e := range s.BlkioStats.IoServiceBytesRecursive {
		switch e.Op {
		case "read", "Read":
			blkRead += e.Value
		case "write", "Write":
			blkWrite += e.Value
		}
	}

	return Sample{
		CPUTotal:   s.CPUStats.CPUUsage.TotalUsage,
		SystemCPU:  s.CPUStats.SystemUsage,
		OnlineCPUs: s.CPUStats.OnlineCPUs,
		NumProcs:   s.NumProcs,
		MemUsage:   s.MemoryStats.Usage,
		MemLimit:   s.MemoryStats.Limit,
		NetRx:      netRx,
		NetTx:      netTx,
		BlkRead:    blkRead,
		BlkWrite:   blkWrite,
		At:         at,
	}
}

// ComputeRates derives rates from two consecutive samples.
// prev may be the zero Sample on the first observation; CPU/IO rates stay 0.
func ComputeRates(prev, curr Sample) model.ContainerRates {
	rates := model.ContainerRates{
		MemUsage:  curr.MemUsage,
		MemLimit:  curr.MemLimit,
		SampledAt: curr.At,
	}

	if prev.At.IsZero() || !curr.At.After(prev.At) {
		return rates
	}

	elapsed := curr.At.Sub(prev.At).Seconds()
	if elapsed <= 0 {
		return rates
	}

	rates.CPUPercent = cpuPercent(prev, curr)
	rates.NetRxBps = float64(curr.NetRx - prev.NetRx) / elapsed
	rates.NetTxBps = float64(curr.NetTx - prev.NetTx) / elapsed
	rates.BlkReadBps = float64(curr.BlkRead - prev.BlkRead) / elapsed
	rates.BlkWriteBps = float64(curr.BlkWrite - prev.BlkWrite) / elapsed
	return rates
}

func cpuPercent(prev, curr Sample) float64 {
	cpuDelta := float64(curr.CPUTotal) - float64(prev.CPUTotal)
	systemDelta := float64(curr.SystemCPU) - float64(prev.SystemCPU)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}

	online := float64(curr.OnlineCPUs)
	if online == 0 {
		online = float64(curr.NumProcs)
	}
	if online == 0 {
		online = 1
	}
	return (cpuDelta / systemDelta) * online * 100.0
}
