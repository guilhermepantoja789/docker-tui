package metrics_test

import (
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/guilhermepantoja789/docker-tui/internal/metrics"
)

func TestComputeRates_FirstSampleHasNoIORates(t *testing.T) {
	curr := metrics.Sample{
		CPUTotal:  100,
		SystemCPU: 1000,
		MemUsage:  50,
		MemLimit:  100,
		NetRx:     10,
		At:        time.Unix(10, 0),
	}
	rates := metrics.ComputeRates(metrics.Sample{}, curr)
	if rates.CPUPercent != 0 {
		t.Fatalf("CPUPercent=%v, want 0 on first sample", rates.CPUPercent)
	}
	if rates.NetRxBps != 0 {
		t.Fatalf("NetRxBps=%v, want 0 on first sample", rates.NetRxBps)
	}
	if rates.MemUsage != 50 || rates.MemLimit != 100 {
		t.Fatalf("mem=%d/%d, want 50/100", rates.MemUsage, rates.MemLimit)
	}
}

func TestComputeRates_Table(t *testing.T) {
	base := time.Unix(100, 0)
	tests := []struct {
		name       string
		prev, curr metrics.Sample
		wantCPU    float64
		wantRxBps  float64
		wantTxBps  float64
	}{
		{
			name: "one_second_delta",
			prev: metrics.Sample{
				CPUTotal: 100, SystemCPU: 1000, OnlineCPUs: 2,
				NetRx: 0, NetTx: 0, BlkRead: 0, BlkWrite: 0,
				At: base,
			},
			curr: metrics.Sample{
				CPUTotal: 200, SystemCPU: 2000, OnlineCPUs: 2,
				NetRx: 1000, NetTx: 500, BlkRead: 200, BlkWrite: 100,
				At: base.Add(time.Second),
			},
			// (100/1000)*2*100 = 20
			wantCPU:   20,
			wantRxBps: 1000,
			wantTxBps: 500,
		},
		{
			name: "zero_system_delta_yields_zero_cpu",
			prev: metrics.Sample{CPUTotal: 100, SystemCPU: 1000, OnlineCPUs: 1, At: base},
			curr: metrics.Sample{CPUTotal: 200, SystemCPU: 1000, OnlineCPUs: 1, At: base.Add(time.Second)},
			wantCPU: 0,
		},
		{
			name: "non_increasing_time_skips_rates",
			prev: metrics.Sample{CPUTotal: 100, SystemCPU: 1000, OnlineCPUs: 1, NetRx: 0, At: base},
			curr: metrics.Sample{CPUTotal: 200, SystemCPU: 2000, OnlineCPUs: 1, NetRx: 999, At: base},
			wantCPU:   0,
			wantRxBps: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := metrics.ComputeRates(tt.prev, tt.curr)
			if got.CPUPercent != tt.wantCPU {
				t.Errorf("CPUPercent=%v, want %v", got.CPUPercent, tt.wantCPU)
			}
			if got.NetRxBps != tt.wantRxBps {
				t.Errorf("NetRxBps=%v, want %v", got.NetRxBps, tt.wantRxBps)
			}
			if got.NetTxBps != tt.wantTxBps {
				t.Errorf("NetTxBps=%v, want %v", got.NetTxBps, tt.wantTxBps)
			}
		})
	}
}

func TestFromStatsResponse_AggregatesNetworksAndBlkio(t *testing.T) {
	s := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{TotalUsage: 42},
			SystemUsage: 100,
			OnlineCPUs:  4,
		},
		MemoryStats: container.MemoryStats{Usage: 7, Limit: 9},
		Networks: map[string]container.NetworkStats{
			"eth0": {RxBytes: 10, TxBytes: 20},
			"eth1": {RxBytes: 5, TxBytes: 6},
		},
		BlkioStats: container.BlkioStats{
			IoServiceBytesRecursive: []container.BlkioStatEntry{
				{Op: "Read", Value: 3},
				{Op: "Write", Value: 4},
				{Op: "read", Value: 1},
			},
		},
	}
	at := time.Unix(1, 0)
	got := metrics.FromStatsResponse(s, at)
	if got.CPUTotal != 42 || got.SystemCPU != 100 || got.OnlineCPUs != 4 {
		t.Fatalf("cpu fields unexpected: %+v", got)
	}
	if got.NetRx != 15 || got.NetTx != 26 {
		t.Fatalf("net rx/tx=%d/%d, want 15/26", got.NetRx, got.NetTx)
	}
	if got.BlkRead != 4 || got.BlkWrite != 4 {
		t.Fatalf("blk r/w=%d/%d, want 4/4", got.BlkRead, got.BlkWrite)
	}
	if !got.At.Equal(at) {
		t.Fatalf("At=%v, want %v", got.At, at)
	}
}
