package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const (
	ComputeStatusReserved  = "reserved"
	ComputeStatusCompleted = "completed"
)

var (
	// ErrComputeBusy means a live reservation already holds this session.
	ErrComputeBusy = errors.New("compute is already running for this session")
	// ErrComputeReservationLost means the caller no longer holds the reservation.
	ErrComputeReservationLost = errors.New("compute reservation is no longer held")
)

// ComputeReceipt is the recorded result of one accepted-session compute.
// A repeat request returns it instead of building or publishing again.
type ComputeReceipt struct {
	SessionID      string    `json:"sessionId"`
	PlanRevisionID string    `json:"planRevisionId"`
	PlanDigest     string    `json:"planDigest"`
	Outcome        string    `json:"outcome"`
	CompletedAt    time.Time `json:"completedAt"`
}

// ComputeReservation is the in-progress lease or the completed receipt for one session.
type ComputeReservation struct {
	SessionID string          `json:"sessionId"`
	Status    string          `json:"status"`
	Token     string          `json:"token,omitempty"`
	ExpiresAt time.Time       `json:"expiresAt,omitempty"`
	Receipt   *ComputeReceipt `json:"receipt,omitempty"`
}

// ReserveComputeExecution inserts a reservation unless a live one exists or a
// receipt is already recorded. An expired reservation can be reclaimed. The
// check and the insert commit together.
func (repository *MemoryRepository) ReserveComputeExecution(_ context.Context, sessionID string, now time.Time, lease time.Duration) (ComputeReservation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.computeExecutions == nil {
		repository.computeExecutions = map[string]ComputeReservation{}
	}
	reservation, err := reserveComputeExecution(repository.computeExecutions, sessionID, now, lease)
	return cloneComputeReservation(reservation), err
}

// RenewComputeExecution extends the lease while the same token still holds it.
func (repository *MemoryRepository) RenewComputeExecution(_ context.Context, sessionID, token string, now time.Time, lease time.Duration) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return renewComputeExecution(repository.computeExecutions, sessionID, token, now, lease)
}

// CompleteComputeExecution records the receipt and the completed status in one
// update. A token that no longer holds the reservation cannot publish a receipt.
func (repository *MemoryRepository) CompleteComputeExecution(_ context.Context, sessionID, token string, receipt ComputeReceipt, now time.Time) (ComputeReservation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	reservation, err := completeComputeExecution(repository.computeExecutions, sessionID, token, receipt, now)
	return cloneComputeReservation(reservation), err
}

// ReleaseComputeExecution drops a reservation held by token. A completed
// receipt is left in place. A different holder's reservation is left in place.
func (repository *MemoryRepository) ReleaseComputeExecution(_ context.Context, sessionID, token string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return releaseComputeExecution(repository.computeExecutions, sessionID, token)
}

func (repository *FileRepository) ReserveComputeExecution(_ context.Context, sessionID string, now time.Time, lease time.Duration) (ComputeReservation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.readComputeExecutions()
	if err != nil {
		return ComputeReservation{}, err
	}
	reservation, err := reserveComputeExecution(records, sessionID, now, lease)
	if err != nil || reservation.Status == ComputeStatusCompleted {
		return reservation, err
	}
	if err := repository.writeComputeExecutions(records); err != nil {
		return ComputeReservation{}, err
	}
	return reservation, nil
}

func (repository *FileRepository) RenewComputeExecution(_ context.Context, sessionID, token string, now time.Time, lease time.Duration) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.readComputeExecutions()
	if err != nil {
		return err
	}
	if err := renewComputeExecution(records, sessionID, token, now, lease); err != nil {
		return err
	}
	return repository.writeComputeExecutions(records)
}

func (repository *FileRepository) CompleteComputeExecution(_ context.Context, sessionID, token string, receipt ComputeReceipt, now time.Time) (ComputeReservation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.readComputeExecutions()
	if err != nil {
		return ComputeReservation{}, err
	}
	reservation, err := completeComputeExecution(records, sessionID, token, receipt, now)
	if err != nil {
		return ComputeReservation{}, err
	}
	if err := repository.writeComputeExecutions(records); err != nil {
		return ComputeReservation{}, err
	}
	return reservation, nil
}

func (repository *FileRepository) ReleaseComputeExecution(_ context.Context, sessionID, token string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	records, err := repository.readComputeExecutions()
	if err != nil {
		return err
	}
	if err := releaseComputeExecution(records, sessionID, token); err != nil {
		return err
	}
	return repository.writeComputeExecutions(records)
}

func (repository *FileRepository) readComputeExecutions() (map[string]ComputeReservation, error) {
	records := map[string]ComputeReservation{}
	if err := readJSON(repository.computeExecutionPath(), &records); err != nil {
		if errors.Is(err, ErrNotFound) {
			return records, nil
		}
		return nil, err
	}
	if records == nil {
		records = map[string]ComputeReservation{}
	}
	return records, nil
}

func (repository *FileRepository) writeComputeExecutions(records map[string]ComputeReservation) error {
	if records == nil {
		records = map[string]ComputeReservation{}
	}
	return atomicWriteJSON(repository.computeExecutionPath(), records)
}

func (repository *FileRepository) computeExecutionPath() string {
	return filepath.Join(repository.root, "compute-executions.json")
}

func reserveComputeExecution(records map[string]ComputeReservation, sessionID string, now time.Time, lease time.Duration) (ComputeReservation, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ComputeReservation{}, errors.New("compute session is required")
	}
	if lease <= 0 {
		return ComputeReservation{}, errors.New("compute lease must be positive")
	}
	now = now.UTC()
	if existing, ok := records[sessionID]; ok {
		if existing.Status == ComputeStatusCompleted && existing.Receipt != nil {
			return cloneComputeReservation(existing), nil
		}
		if existing.Status == ComputeStatusReserved && existing.ExpiresAt.After(now) {
			return ComputeReservation{}, ErrComputeBusy
		}
	}
	token, err := randomComputeToken()
	if err != nil {
		return ComputeReservation{}, err
	}
	reservation := ComputeReservation{
		SessionID: sessionID,
		Status:    ComputeStatusReserved,
		Token:     token,
		ExpiresAt: now.Add(lease),
	}
	records[sessionID] = reservation
	return cloneComputeReservation(reservation), nil
}

func renewComputeExecution(records map[string]ComputeReservation, sessionID, token string, now time.Time, lease time.Duration) error {
	existing, ok := records[sessionID]
	if !ok || existing.Status != ComputeStatusReserved || existing.Token == "" || existing.Token != token {
		return ErrComputeReservationLost
	}
	if lease <= 0 {
		return errors.New("compute lease must be positive")
	}
	existing.ExpiresAt = now.UTC().Add(lease)
	records[sessionID] = existing
	return nil
}

func completeComputeExecution(records map[string]ComputeReservation, sessionID, token string, receipt ComputeReceipt, now time.Time) (ComputeReservation, error) {
	existing, ok := records[sessionID]
	if !ok || existing.Status != ComputeStatusReserved || existing.Token == "" || existing.Token != token {
		return ComputeReservation{}, ErrComputeReservationLost
	}
	receipt.SessionID = sessionID
	if receipt.CompletedAt.IsZero() {
		receipt.CompletedAt = now.UTC()
	}
	existing.Status = ComputeStatusCompleted
	existing.Token = ""
	existing.ExpiresAt = time.Time{}
	existing.Receipt = &receipt
	records[sessionID] = existing
	return cloneComputeReservation(existing), nil
}

func releaseComputeExecution(records map[string]ComputeReservation, sessionID, token string) error {
	existing, ok := records[sessionID]
	if !ok || existing.Status != ComputeStatusReserved {
		return nil
	}
	if existing.Token != token {
		return nil
	}
	delete(records, sessionID)
	return nil
}

func cloneComputeReservation(reservation ComputeReservation) ComputeReservation {
	if reservation.Receipt != nil {
		receipt := *reservation.Receipt
		reservation.Receipt = &receipt
	}
	return reservation
}

func randomComputeToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
