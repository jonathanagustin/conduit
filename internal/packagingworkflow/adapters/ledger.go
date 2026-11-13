// Package adapters holds the worker-local files behind packaging activities.
// The approval file is a read model of a decision Conduit governance already
// made. The handoff file records the Plan id the delivery kernel returned so
// a retry can reconcile. Neither file is an approval database or a lease table.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// ErrDenied is a terminal execution-control refusal. The handoff must not
// retry it into a submission.
var ErrDenied = errors.New("execution control denied the handoff")

// Ledger is the worker-local store for step receipts, the approval read model,
// and the Plan-id journal. The workflow history stores the receipts the
// activities return, not these files.
type Ledger struct {
	dir string
	mu  sync.Mutex
}

func Open(dir string) (*Ledger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Ledger{dir: dir}, nil
}

func (l *Ledger) LoadReceipt(_ context.Context, key string) (domain.Receipt, bool, error) {
	unlock, err := l.lock()
	if err != nil {
		return domain.Receipt{}, false, err
	}
	defer unlock()
	receipts, err := readMap[domain.Receipt](l.path("receipts.json"))
	if err != nil {
		return domain.Receipt{}, false, err
	}
	receipt, ok := receipts[key]
	return receipt, ok, nil
}

func (l *Ledger) StoreReceipt(_ context.Context, key string, receipt domain.Receipt) (domain.Receipt, error) {
	if err := receipt.Bound(); err != nil {
		return domain.Receipt{}, err
	}
	unlock, err := l.lock()
	if err != nil {
		return domain.Receipt{}, err
	}
	defer unlock()
	receipts, err := readMap[domain.Receipt](l.path("receipts.json"))
	if err != nil {
		return domain.Receipt{}, err
	}
	if existing, ok := receipts[key]; ok {
		return existing, nil
	}
	receipt.IdempotencyKey = key
	receipts[key] = receipt
	if err := writeMap(l.path("receipts.json"), receipts); err != nil {
		return domain.Receipt{}, err
	}
	return receipt, nil
}

func (l *Ledger) ReadApproval(_ context.Context, approvalID string) (domain.ApprovalRecord, bool, error) {
	unlock, err := l.lock()
	if err != nil {
		return domain.ApprovalRecord{}, false, err
	}
	defer unlock()
	records, err := readMap[domain.ApprovalRecord](l.path("approvals.json"))
	if err != nil {
		return domain.ApprovalRecord{}, false, err
	}
	record, ok := records[approvalID]
	return record, ok, nil
}

// PutApproval records a decision that Conduit governance has already made.
// Calling it is the adapter's job after that decision. It does not approve.
func (l *Ledger) PutApproval(record domain.ApprovalRecord) error {
	if record.ApprovalID == "" {
		return errors.New("approval id is required")
	}
	unlock, err := l.lock()
	if err != nil {
		return err
	}
	defer unlock()
	records, err := readMap[domain.ApprovalRecord](l.path("approvals.json"))
	if err != nil {
		return err
	}
	if existing, ok := records[record.ApprovalID]; ok {
		if existing != record {
			return errors.New("approval id already records a different decision")
		}
		return nil
	}
	records[record.ApprovalID] = record
	return writeMap(l.path("approvals.json"), records)
}

func (l *Ledger) Reconcile(_ context.Context, key string) (domain.PlanReceipt, bool, error) {
	unlock, err := l.lock()
	if err != nil {
		return domain.PlanReceipt{}, false, err
	}
	defer unlock()
	plans, err := readMap[domain.PlanReceipt](l.path("handoff.json"))
	if err != nil {
		return domain.PlanReceipt{}, false, err
	}
	receipt, ok := plans[key]
	if ok {
		receipt.Reconciled = true
	}
	return receipt, ok, nil
}

// Remember records the Plan id the delivery kernel returned. A second call for
// the same key keeps the first receipt. This journal is not the Plan store.
func (l *Ledger) Remember(_ context.Context, key string, receipt domain.PlanReceipt) error {
	if key == "" || receipt.PlanID == "" {
		return errors.New("plan journal requires a key and a plan id")
	}
	unlock, err := l.lock()
	if err != nil {
		return err
	}
	defer unlock()
	plans, err := readMap[domain.PlanReceipt](l.path("handoff.json"))
	if err != nil {
		return err
	}
	if existing, ok := plans[key]; ok {
		if existing.PlanID != receipt.PlanID {
			return errors.New("plan journal already records a different plan")
		}
		return nil
	}
	receipt.Key = key
	plans[key] = receipt
	return writeMap(l.path("handoff.json"), plans)
}

func (l *Ledger) path(name string) string {
	return filepath.Join(l.dir, name)
}

func (l *Ledger) lock() (func(), error) {
	l.mu.Lock()
	file, err := os.OpenFile(l.path("ledger.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		l.mu.Unlock()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		l.mu.Unlock()
	}, nil
}

func readMap[T any](path string) (map[string]T, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]T{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]T{}
	}
	return out, nil
}

func writeMap[T any](path string, value map[string]T) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
