package app

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type HealthStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsedGB   float64 `json:"mem_used_gb"`
	MemTotalGB  float64 `json:"mem_total_gb"`
	MemPercent  float64 `json:"mem_percent"`
	DiskUsedGB  float64 `json:"disk_used_gb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskPercent float64 `json:"disk_percent"`
}

var cpuIdleRe = regexp.MustCompile(`(\d+\.\d+)%\s+idle`)

func collectCPU() float64 {
	out, err := exec.Command("sh", "-c", `top -l 2 -n 0 -s 1 | grep "CPU usage" | tail -1`).Output()
	if err != nil {
		return 0
	}
	m := cpuIdleRe.FindStringSubmatch(string(out))
	if len(m) < 2 {
		return 0
	}
	idle, _ := strconv.ParseFloat(m[1], 64)
	return 100 - idle
}

func collectMemory() (used, total, pct float64) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return
	}
	totalBytes, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	total = float64(totalBytes) / (1 << 30)

	vmOut, err := exec.Command("vm_stat").Output()
	if err != nil {
		return
	}
	const pageSize = int64(4096)
	var active, wired, compressed int64
	for _, line := range strings.Split(string(vmOut), "\n") {
		switch {
		case strings.Contains(line, "Pages active"):
			active = parseVMStatLine(line)
		case strings.Contains(line, "Pages wired down"):
			wired = parseVMStatLine(line)
		case strings.Contains(line, "Pages occupied by compressor"):
			compressed = parseVMStatLine(line)
		}
	}
	used = float64((active+wired+compressed)*pageSize) / (1 << 30)
	if total > 0 {
		pct = used / total * 100
	}
	return
}

func parseVMStatLine(line string) int64 {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimRight(parts[len(parts)-1], "."), 10, 64)
	return v
}

func collectDisk() (used, total, pct float64) {
	out, err := exec.Command("df", "-k", "/").Output()
	if err != nil {
		return
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 3 {
		return
	}
	totalKB, _ := strconv.ParseInt(fields[1], 10, 64)
	usedKB, _ := strconv.ParseInt(fields[2], 10, 64)
	total = float64(totalKB) / (1 << 20)
	used = float64(usedKB) / (1 << 20)
	if total > 0 {
		pct = used / total * 100
	}
	return
}

func handleHealthAPI(auth *AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := auth.CurrentUserOrDev(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		type result struct {
			cpu         float64
			mu, mt, mp float64
			du, dt, dp float64
		}

		cpuCh := make(chan float64, 1)
		memCh := make(chan [3]float64, 1)
		diskCh := make(chan [3]float64, 1)

		go func() { cpuCh <- collectCPU() }()
		go func() { u, t, p := collectMemory(); memCh <- [3]float64{u, t, p} }()
		go func() { u, t, p := collectDisk(); diskCh <- [3]float64{u, t, p} }()

		cpu := <-cpuCh
		mem := <-memCh
		disk := <-diskCh

		stats := HealthStats{
			CPUPercent:  cpu,
			MemUsedGB:   mem[0],
			MemTotalGB:  mem[1],
			MemPercent:  mem[2],
			DiskUsedGB:  disk[0],
			DiskTotalGB: disk[1],
			DiskPercent: disk[2],
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}
