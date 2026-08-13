package optimization

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type FileEvaluatorStore struct{ Directory string }

func (store FileEvaluatorStore) SaveRun(run EvaluatorRun) error {
	if err := VerifyEvaluatorRun(run); err != nil {
		return err
	}
	return FileTwinStore(store).save("evaluator-runs", run.ID, run)
}

func (store FileEvaluatorStore) GetRun(id string) (EvaluatorRun, error) {
	var run EvaluatorRun
	if !validContentID(id, "eval_") {
		return run, errors.New("invalid evaluator run id")
	}
	if err := FileTwinStore(store).get("evaluator-runs", id, &run); err != nil {
		return run, err
	}
	return run, VerifyEvaluatorRun(run)
}

func (store FileEvaluatorStore) SaveBatch(batch EvaluatorBatch) error {
	replayed, err := NewEvaluatorBatch(batch.Runs, batch.Dispositions)
	if err != nil || !reflect.DeepEqual(replayed, batch) {
		return errors.New("invalid evaluator batch")
	}
	return FileTwinStore(store).save("evaluator-batches", batch.ID, batch)
}

func (store FileEvaluatorStore) GetBatch(id string) (EvaluatorBatch, error) {
	var batch EvaluatorBatch
	if !validContentID(id, "evb_") {
		return batch, errors.New("invalid evaluator batch id")
	}
	if err := FileTwinStore(store).get("evaluator-batches", id, &batch); err != nil {
		return batch, err
	}
	replayed, err := NewEvaluatorBatch(batch.Runs, batch.Dispositions)
	if err != nil || !reflect.DeepEqual(replayed, batch) {
		return EvaluatorBatch{}, errors.New("invalid stored evaluator batch")
	}
	return batch, nil
}

func (store FileEvaluatorStore) SaveResolution(batch EvaluatorBatch, resolution EvaluatorResolution) error {
	if err := VerifyEvaluatorResolution(batch, resolution); err != nil {
		return err
	}
	return FileTwinStore(store).save("evaluator-resolutions", resolution.ID, resolution)
}

func (store FileEvaluatorStore) GetResolution(batch EvaluatorBatch, id string) (EvaluatorResolution, error) {
	var resolution EvaluatorResolution
	if !validContentID(id, "evr_") {
		return resolution, errors.New("invalid evaluator resolution id")
	}
	if err := FileTwinStore(store).get("evaluator-resolutions", id, &resolution); err != nil {
		return resolution, err
	}
	return resolution, VerifyEvaluatorResolution(batch, resolution)
}

func (store FileEvaluatorStore) ListResolutions(batch EvaluatorBatch) ([]EvaluatorResolution, error) {
	directory := filepath.Join(store.Directory, "evaluator-resolutions")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []EvaluatorResolution{}, nil
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") && validContentID(id, "evr_") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	resolutions := make([]EvaluatorResolution, 0, len(ids))
	for _, id := range ids {
		var resolution EvaluatorResolution
		if err := FileTwinStore(store).get("evaluator-resolutions", id, &resolution); err != nil {
			return nil, err
		}
		if resolution.BatchID != batch.ID {
			continue
		}
		if err := VerifyEvaluatorResolution(batch, resolution); err != nil {
			return nil, err
		}
		resolutions = append(resolutions, resolution)
	}
	return resolutions, nil
}
