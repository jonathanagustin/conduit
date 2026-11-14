// Package domain is a staged rollout. Each wave waits on one gate: a healthy
// report, a recorded approval, or an open time window. The next wave does not
// start until that gate passes.
package domain

import (
	"fmt"
	"strings"
)

const (
	StatusDraft      = "draft"
	StatusRunning    = "running"
	StatusPaused     = "paused"
	StatusRolledBack = "rolled_back"
	StatusCompleted  = "completed"

	WavePending = "pending"
	WaveCurrent = "current"
	WavePassed  = "passed"
	WaveBlocked = "blocked"

	GateHealth   = "health"
	GateApproval = "approval"
	GateWindow   = "window"
)

// Wave is one stage.
type Wave struct {
	Name         string   `json:"name"`
	Environments []string `json:"environments"`
	Gate         string   `json:"gate"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
}

// Rollout is the staged plan.
type Rollout struct {
	ID       string `json:"id"`
	Product  string `json:"product"`
	Version  string `json:"version"`
	Status   string `json:"status"`
	Author   string `json:"author"`
	Waves    []Wave `json:"waves"`
	PausedBy string `json:"pausedBy,omitempty"`
}

// Facts are the observations a gate reads. Zero values fail closed.
type Facts struct {
	Healthy  map[string]bool
	Approved bool
	Window   bool
}

// New validates a rollout and leaves every wave pending.
func New(id, product, version, author string, waves []Wave) (Rollout, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(product) == "" || strings.TrimSpace(version) == "" || strings.TrimSpace(author) == "" || len(waves) == 0 {
		return Rollout{}, fmt.Errorf("a rollout needs an id, product, version, author, and at least one wave")
	}
	rollout := Rollout{ID: id, Product: product, Version: version, Author: author, Status: StatusDraft}
	for _, wave := range waves {
		if strings.TrimSpace(wave.Name) == "" || len(wave.Environments) == 0 {
			return Rollout{}, fmt.Errorf("wave %q needs a name and an environment", wave.Name)
		}
		switch wave.Gate {
		case GateHealth, GateApproval, GateWindow:
		default:
			return Rollout{}, fmt.Errorf("wave %q has an unknown gate %q", wave.Name, wave.Gate)
		}
		wave.Status = WavePending
		wave.Reason = ""
		rollout.Waves = append(rollout.Waves, wave)
	}
	return rollout, nil
}

// Advance starts the next pending wave when its gate passes. A closed gate
// blocks that wave and names the reason. Later waves stay pending.
func Advance(rollout Rollout, facts Facts) (Rollout, string) {
	rollout.Waves = append([]Wave(nil), rollout.Waves...)
	if rollout.Status == StatusPaused || rollout.Status == StatusRolledBack || rollout.Status == StatusCompleted {
		return rollout, "rollout is " + rollout.Status
	}
	index := -1
	for i := range rollout.Waves {
		if rollout.Waves[i].Status == WavePending || rollout.Waves[i].Status == WaveBlocked || rollout.Waves[i].Status == WaveCurrent {
			index = i
			break
		}
	}
	if index < 0 {
		rollout.Status = StatusCompleted
		return rollout, "every wave has passed"
	}
	for i := 0; i < index; i++ {
		if rollout.Waves[i].Status != WavePassed {
			return rollout, "an earlier wave has not passed"
		}
	}
	reason := gateReason(rollout.Waves[index], facts)
	if reason != "" {
		rollout.Waves[index].Status = WaveBlocked
		rollout.Waves[index].Reason = reason
		rollout.Status = StatusRunning
		return rollout, reason
	}
	if rollout.Waves[index].Status != WaveCurrent {
		rollout.Waves[index].Status = WaveCurrent
		rollout.Waves[index].Reason = ""
		rollout.Status = StatusRunning
		return rollout, "wave " + rollout.Waves[index].Name + " started"
	}
	rollout.Waves[index].Status = WavePassed
	rollout.Waves[index].Reason = ""
	if index == len(rollout.Waves)-1 {
		rollout.Status = StatusCompleted
		return rollout, "every wave has passed"
	}
	return Advance(rollout, facts)
}

func gateReason(wave Wave, facts Facts) string {
	switch wave.Gate {
	case GateHealth:
		var missing []string
		for _, environment := range wave.Environments {
			if !facts.Healthy[environment] {
				missing = append(missing, environment)
			}
		}
		if len(missing) > 0 {
			return "health gate is closed for " + strings.Join(missing, ", ")
		}
	case GateApproval:
		if !facts.Approved {
			return "approval gate is missing"
		}
	case GateWindow:
		if !facts.Window {
			return "time window is closed"
		}
	default:
		return "wave gate is unknown"
	}
	return ""
}

// Pause stops a running rollout. The caller must have confirmed.
func Pause(rollout Rollout, actor string, confirmed bool) (Rollout, string) {
	rollout.Waves = append([]Wave(nil), rollout.Waves...)
	if !confirmed {
		return rollout, "confirmation is required"
	}
	if rollout.Status != StatusRunning {
		return rollout, "rollout is not running"
	}
	rollout.Status = StatusPaused
	rollout.PausedBy = actor
	return rollout, "rollout paused"
}
