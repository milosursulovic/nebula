package agent

import "testing"

func TestParseProcStat(t *testing.T) {
	content := "cpu  100 0 200 700 0 0 0 0 0 0\ncpu0 50 0 100 350 0 0 0 0 0 0\n"
	sample, err := parseProcStat(content)
	if err != nil {
		t.Fatalf("parseProcStat: %v", err)
	}
	if sample.idle != 700 {
		t.Errorf("idle = %d, want 700", sample.idle)
	}
	if sample.total != 1000 {
		t.Errorf("total = %d, want 1000", sample.total)
	}
}

func TestParseProcStatMissingCPULine(t *testing.T) {
	if _, err := parseProcStat("not stat content\n"); err == nil {
		t.Error("expected error for content with no cpu line")
	}
}

func TestCPUUsagePercent(t *testing.T) {
	prev := cpuSample{idle: 700, total: 1000}
	cur := cpuSample{idle: 750, total: 1200} // total +200, idle +50 -> 75% busy
	got := cpuUsagePercent(prev, cur)
	if got != 75 {
		t.Errorf("cpuUsagePercent = %v, want 75", got)
	}
}

func TestCPUUsagePercentNoDelta(t *testing.T) {
	s := cpuSample{idle: 700, total: 1000}
	if got := cpuUsagePercent(s, s); got != 0 {
		t.Errorf("cpuUsagePercent with no delta = %v, want 0", got)
	}
}

func TestParseMemInfo(t *testing.T) {
	content := "MemTotal:       16000000 kB\nMemFree:         2000000 kB\nMemAvailable:    6000000 kB\n"
	usedMB, err := parseMemInfo(content)
	if err != nil {
		t.Fatalf("parseMemInfo: %v", err)
	}
	want := (16000000 - 6000000) / 1024
	if usedMB != want {
		t.Errorf("usedMB = %d, want %d", usedMB, want)
	}
}

func TestParseMemInfoMissingFields(t *testing.T) {
	if _, err := parseMemInfo("SomeOtherField: 1 kB\n"); err == nil {
		t.Error("expected error when MemTotal/MemAvailable are missing")
	}
}

func TestParseLoadAvg(t *testing.T) {
	got, err := parseLoadAvg("2.13 1.98 1.75 3/819 12345\n")
	if err != nil {
		t.Fatalf("parseLoadAvg: %v", err)
	}
	if got != 2.13 {
		t.Errorf("loadAvg = %v, want 2.13", got)
	}
}

func TestParseLoadAvgEmpty(t *testing.T) {
	if _, err := parseLoadAvg(""); err == nil {
		t.Error("expected error for empty /proc/loadavg content")
	}
}
