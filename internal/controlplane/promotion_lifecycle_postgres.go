package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// The context-owned table is the lifecycle authority. The legacy snapshot must
// remain readable/writable by older binaries, whose struct decoding drops new
// fields. Hydration occurs inside the same transaction as every metadata read
// or write, so a rolling old writer cannot erase a terminal run's history.
func loadPromotionLifecycles(ctx context.Context, transaction *sql.Tx, memory *MemoryRepository) error {
	rows, err := transaction.QueryContext(ctx, `SELECT id, run_id, previous_id, lifecycle, reason, value_tier FROM conduit_promotion_lifecycle`)
	if err != nil {
		return fmt.Errorf("read promotion lifecycles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record PromotionRecord
		if err := rows.Scan(&record.ID, &record.RunID, &record.PreviousEvaluationID, &record.Lifecycle, &record.LifecycleReason, &record.ValueTier); err != nil {
			return err
		}
		if existing, ok := memory.promotions[record.ID]; ok {
			existing.RunID, existing.PreviousEvaluationID = record.RunID, record.PreviousEvaluationID
			existing.Lifecycle, existing.LifecycleReason, existing.ValueTier = record.Lifecycle, record.LifecycleReason, record.ValueTier
			memory.promotions[record.ID] = existing
		}
	}
	return rows.Err()
}

func storePromotionLifecycles(ctx context.Context, transaction *sql.Tx, memory *MemoryRepository) error {
	for _, record := range memory.promotions {
		if record.RunID == "" {
			continue
		}
		_, err := transaction.ExecContext(ctx, `INSERT INTO conduit_promotion_lifecycle (id, run_id, previous_id, lifecycle, reason, value_tier)
   VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (id) DO NOTHING`, record.ID, record.RunID, record.PreviousEvaluationID, record.Lifecycle, record.LifecycleReason, record.ValueTier)
		if err != nil {
			return fmt.Errorf("persist promotion lifecycle: %w", err)
		}
	}
	return nil
}

func encodeLegacyPromotionState(memory *MemoryRepository) ([]byte, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	state := memory.stateLocked()
	state.Promotions = make(map[string]PromotionRecord, len(memory.promotions))
	for id, record := range memory.promotions {
		record.RunID, record.PreviousEvaluationID = "", ""
		record.Lifecycle, record.LifecycleReason, record.ValueTier = "", "", ""
		state.Promotions[id] = record
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode legacy promotion state: %w", err)
	}
	return encoded, nil
}
