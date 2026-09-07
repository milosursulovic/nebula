package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Metrics is one heartbeat's worth of host resource usage (spec section
// 11's heartbeat body).
type Metrics struct {
	CPUUsage    float64
	MemUsedMB   int
	DiskUsedGB  int
	LoadAverage float64
}

// cpuSample is /proc/stat's first ("cpu") line, reduced to what the
// delta-based usage calculation needs.
type cpuSample struct {
	idle  uint64
	total uint64
}

// parseProcStat reads /proc/stat's aggregate "cpu" line into a cpuSample.
// Split out from file I/O so it's unit-testable against fixed sample text
// rather than depending on the host's actual /proc.
func parseProcStat(content string) (cpuSample, error) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var total uint64
		vals := make([]uint64, 0, len(fields)-1)
		for _, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuSample{}, fmt.Errorf("parse /proc/stat cpu field %q: %w", f, err)
			}
			vals = append(vals, v)
			total += v
		}
		// Field order: user nice system idle iowait irq softirq steal ...
		return cpuSample{idle: vals[3], total: total}, nil
	}
	return cpuSample{}, fmt.Errorf("no cpu line found in /proc/stat content")
}

// cpuUsagePercent computes utilization from two samples taken apart in
// time (the standard /proc/stat delta method — a single snapshot can't
// tell you a rate).
func cpuUsagePercent(prev, cur cpuSample) float64 {
	totalDelta := cur.total - prev.total
	if totalDelta == 0 {
		return 0
	}
	idleDelta := cur.idle - prev.idle
	return (float64(totalDelta) - float64(idleDelta)) / float64(totalDelta) * 100
}

// parseMemInfo reads /proc/meminfo's MemTotal/MemAvailable (kB) into a
// used-memory figure in MB.
func parseMemInfo(content string) (usedMB int, err error) {
	var totalKB, availableKB int64
	haveTotal, haveAvailable := false, false

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB, err = strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse MemTotal: %w", err)
			}
			haveTotal = true
		case "MemAvailable:":
			availableKB, err = strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse MemAvailable: %w", err)
			}
			haveAvailable = true
		}
	}
	if !haveTotal || !haveAvailable {
		return 0, fmt.Errorf("MemTotal/MemAvailable not found in /proc/meminfo content")
	}
	return int((totalKB - availableKB) / 1024), nil
}

// parseLoadAvg reads /proc/loadavg's 1-minute load average (first field).
func parseLoadAvg(content string) (float64, error) {
	fields := strings.Fields(content)
	if len(fields) < 1 {
		return 0, fmt.Errorf("empty /proc/loadavg content")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse /proc/loadavg 1-minute field: %w", err)
	}
	return v, nil
}

// diskUsedGB reports used space (GB) on path via statfs — real host
// numbers, no mocking (only VM operations are mocked this phase).
func diskUsedGB(path string) (int, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	usedBytes := (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	return int(usedBytes / (1000 * 1000 * 1000)), nil
}

// collectMetrics samples real host resource usage. CPU usage needs two
// /proc/stat reads apart in time, so this takes ~200ms.
func collectMetrics(ctx context.Context) (Metrics, error) {
	before, err := readProcStat()
	if err != nil {
		return Metrics{}, err
	}

	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		return Metrics{}, ctx.Err()
	}

	after, err := readProcStat()
	if err != nil {
		return Metrics{}, err
	}

	memUsedMB, err := readMemInfo()
	if err != nil {
		return Metrics{}, err
	}

	loadAvg, err := readLoadAvg()
	if err != nil {
		return Metrics{}, err
	}

	diskUsed, err := diskUsedGB("/")
	if err != nil {
		return Metrics{}, err
	}

	return Metrics{
		CPUUsage:    cpuUsagePercent(before, after),
		MemUsedMB:   memUsedMB,
		DiskUsedGB:  diskUsed,
		LoadAverage: loadAvg,
	}, nil
}

func readProcStat() (cpuSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, fmt.Errorf("read /proc/stat: %w", err)
	}
	return parseProcStat(string(data))
}

func readMemInfo() (int, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return parseMemInfo(string(data))
}

func readLoadAvg() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, fmt.Errorf("read /proc/loadavg: %w", err)
	}
	return parseLoadAvg(string(data))
}
