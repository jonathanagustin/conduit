package domain

// AppendHistory validates an optimistic append and returns an independent
// canonical history. Storage adapters must compare and write inside one atomic
// boundary; invoking this helper before an unrelated write is insufficient.
func AppendHistory(history []Event, expected uint64, next Event) ([]Event, error) {
	if len(history) > 3 {
		return nil, ErrCorrupt
	}
	if len(history) > 0 {
		if _, err := ValidateHistory(history); err != nil {
			return nil, ErrCorrupt
		}
	}
	if expected != uint64(len(history)) {
		return nil, ErrConflict
	}
	if next.Revision != expected+1 {
		return nil, ErrInvalid
	}
	document, err := EncodeEvent(next)
	if err != nil {
		return nil, err
	}
	next, err = DecodeEvent(next.SessionID, next.Revision, document)
	if err != nil {
		return nil, ErrInvalid
	}
	result := make([]Event, len(history)+1)
	copy(result, history)
	result[len(history)] = next
	if _, err := ValidateHistory(result); err != nil {
		return nil, ErrInvalid
	}
	return result, nil
}
