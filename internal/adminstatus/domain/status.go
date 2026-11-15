// Package domain defines non-authoritative, read-only platform measurements.
package domain

import "time"

const (
	Available       = "AVAILABLE"
	Unavailable     = "UNAVAILABLE"
	WarningPercent  = 80.0
	CriticalPercent = 95.0
)

type Counter struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Value  *int64 `json:"value"`
	Detail string `json:"detail"`
	Link   string `json:"link"`
}

func Count(id, label string, value int64, detail, link string) Counter {
	return Counter{ID: id, Label: label, Status: Available, Value: &value, Detail: detail, Link: link}
}
func MissingCount(id, label, detail, link string) Counter {
	return Counter{ID: id, Label: label, Status: Unavailable, Detail: detail, Link: link}
}

type Component struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type Storage struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Status          string   `json:"status"`
	UsedBytes       *int64   `json:"usedBytes"`
	CapacityBytes   *int64   `json:"capacityBytes"`
	UsedPercent     *float64 `json:"usedPercent"`
	WarningPercent  float64  `json:"warningPercent"`
	CriticalPercent float64  `json:"criticalPercent"`
	Detail          string   `json:"detail"`
}

func Capacity(id, label string, used, total int64) Storage {
	result := MissingStorage(id, label, "Measurement unavailable")
	if total <= 0 || used < 0 || used > total {
		return result
	}
	percent := 100 * float64(used) / float64(total)
	result.UsedBytes = &used
	result.CapacityBytes = &total
	result.UsedPercent = &percent
	result.Status = "OK"
	result.Detail = "Backing filesystem usage; not the directory's own byte count"
	if percent >= CriticalPercent {
		result.Status = "CRITICAL"
	} else if percent >= WarningPercent {
		result.Status = "WARNING"
	}
	return result
}
func MissingStorage(id, label, detail string) Storage {
	return Storage{ID: id, Label: label, Status: Unavailable, WarningPercent: WarningPercent, CriticalPercent: CriticalPercent, Detail: detail}
}

type Audit struct {
	Status      string     `json:"status"`
	ChainLength *uint64    `json:"chainLength"`
	VerifiedAt  *time.Time `json:"verifiedAt"`
	Detail      string     `json:"detail"`
}
type Summary struct {
	GeneratedAt time.Time   `json:"generatedAt"`
	Components  []Component `json:"components"`
	Counters    []Counter   `json:"counters"`
	Storage     []Storage   `json:"storage"`
	Audit       Audit       `json:"audit"`
}
