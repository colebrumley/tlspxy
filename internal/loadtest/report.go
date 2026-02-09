package loadtest

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// PrintText writes a formatted text report for a single Result.
func PrintText(w io.Writer, r Result) {
	fmt.Fprintf(w, "=== %s (%s, %d workers) ===\n", r.Scenario, formatDurationShort(r.Duration), r.Concurrency)
	fmt.Fprintf(w, "  Throughput:    %s req/s | %.2f MB/s\n", formatFloat(r.RequestsPerSec), r.ThroughputMBps)
	fmt.Fprintf(w, "  Latency p50/p95/p99/max: %s / %s / %s / %s\n",
		formatMs(r.RoundTripP50), formatMs(r.RoundTripP95), formatMs(r.RoundTripP99), formatMs(r.RoundTripMax))
	fmt.Fprintf(w, "  Connect p50/p95/p99: %s / %s / %s\n",
		formatMs(r.ConnectP50), formatMs(r.ConnectP95), formatMs(r.ConnectP99))
	fmt.Fprintf(w, "  Errors: %s (%.2f%%)\n", formatInt(r.ErrorCount), r.ErrorRate*100)
	if len(r.ErrorBreakdown) > 0 {
		for errType, count := range r.ErrorBreakdown {
			fmt.Fprintf(w, "    - %s: %s\n", errType, formatInt(int64(count)))
		}
	}
}

// PrintJSON writes the results slice as a JSON array to w.
func PrintJSON(w io.Writer, results []Result) error {
	type jsonResult struct {
		Scenario       string           `json:"scenario"`
		Duration       string           `json:"duration"`
		Concurrency    int              `json:"concurrency"`
		TotalRequests  int64            `json:"total_requests"`
		RequestsPerSec float64          `json:"requests_per_sec"`
		BytesSent      int64            `json:"bytes_sent"`
		BytesReceived  int64            `json:"bytes_received"`
		ThroughputMBps float64          `json:"throughput_mbps"`
		ConnectP50Ms   float64          `json:"connect_p50_ms"`
		ConnectP95Ms   float64          `json:"connect_p95_ms"`
		ConnectP99Ms   float64          `json:"connect_p99_ms"`
		RoundTripP50Ms float64          `json:"roundtrip_p50_ms"`
		RoundTripP95Ms float64          `json:"roundtrip_p95_ms"`
		RoundTripP99Ms float64          `json:"roundtrip_p99_ms"`
		RoundTripMaxMs float64          `json:"roundtrip_max_ms"`
		ErrorCount     int64            `json:"error_count"`
		ErrorRate      float64          `json:"error_rate"`
		ErrorBreakdown map[string]int `json:"error_breakdown,omitempty"`
	}

	out := make([]jsonResult, len(results))
	for i, r := range results {
		out[i] = jsonResult{
			Scenario:       r.Scenario,
			Duration:       r.Duration.String(),
			Concurrency:    r.Concurrency,
			TotalRequests:  r.TotalRequests,
			RequestsPerSec: r.RequestsPerSec,
			BytesSent:      r.BytesSent,
			BytesReceived:  r.BytesReceived,
			ThroughputMBps: r.ThroughputMBps,
			ConnectP50Ms:   durationMs(r.ConnectP50),
			ConnectP95Ms:   durationMs(r.ConnectP95),
			ConnectP99Ms:   durationMs(r.ConnectP99),
			RoundTripP50Ms: durationMs(r.RoundTripP50),
			RoundTripP95Ms: durationMs(r.RoundTripP95),
			RoundTripP99Ms: durationMs(r.RoundTripP99),
			RoundTripMaxMs: durationMs(r.RoundTripMax),
			ErrorCount:     r.ErrorCount,
			ErrorRate:      r.ErrorRate,
			ErrorBreakdown: r.ErrorBreakdown,
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// PrintSummary writes a summary table of all results.
func PrintSummary(w io.Writer, results []Result) {
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Summary")
	fmt.Fprintln(w, strings.Repeat("─", 100))
	fmt.Fprintf(w, "%-20s %10s %10s %10s %10s %10s %10s\n",
		"Scenario", "Req/s", "MB/s", "P50", "P99", "Errors", "Err%")
	fmt.Fprintln(w, strings.Repeat("─", 100))

	for _, r := range results {
		fmt.Fprintf(w, "%-20s %10s %10.2f %10s %10s %10s %9.2f%%\n",
			truncate(r.Scenario, 20),
			formatFloat(r.RequestsPerSec),
			r.ThroughputMBps,
			formatMs(r.RoundTripP50),
			formatMs(r.RoundTripP99),
			formatInt(r.ErrorCount),
			r.ErrorRate*100,
		)
	}
	fmt.Fprintln(w, strings.Repeat("─", 100))
}

// formatMs formats a duration as milliseconds with 2 decimal places.
func formatMs(d time.Duration) string {
	return fmt.Sprintf("%.2fms", durationMs(d))
}

// durationMs converts a duration to fractional milliseconds.
func durationMs(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / float64(time.Millisecond)
}

// formatFloat formats a float with commas as thousands separators.
func formatFloat(f float64) string {
	whole := fmt.Sprintf("%.1f", f)
	parts := strings.SplitN(whole, ".", 2)
	intPart := addCommas(parts[0])
	if len(parts) == 2 {
		return intPart + "." + parts[1]
	}
	return intPart
}

// formatInt formats an integer with commas as thousands separators.
func formatInt(n int64) string {
	return addCommas(fmt.Sprintf("%d", n))
}

// addCommas inserts commas into a numeric string for readability.
func addCommas(s string) string {
	if len(s) == 0 {
		return s
	}

	start := 0
	if s[0] == '-' {
		start = 1
	}
	digits := s[start:]
	n := len(digits)
	if n <= 3 {
		return s
	}

	var b strings.Builder
	b.WriteString(s[:start])
	first := n % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(digits[:first])
	for i := first; i < n; i += 3 {
		b.WriteByte(',')
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// formatDurationShort returns a compact human-readable duration string.
func formatDurationShort(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.0fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.0fm", d.Minutes())
	case d >= time.Second:
		return fmt.Sprintf("%.0fs", d.Seconds())
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

// truncate shortens a string to maxLen, adding "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
