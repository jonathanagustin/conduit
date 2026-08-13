package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func (repository *MemoryRepository) PutLabelDefinition(_ context.Context, definition LabelDefinition) (LabelDefinition, error) {
	if err := validateLabelDefinition(definition, true); err != nil {
		return LabelDefinition{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	existing, exists := repository.labelDefinitions[definition.ID]
	if (!exists && definition.Revision != 1) || (exists && definition.Revision != existing.Revision+1) ||
		(exists && existing.Status == LabelDefinitionTombstoned) {
		return LabelDefinition{}, ErrConflict
	}
	plans, teams, pipelines := []PlanRecord{}, []Team{}, []ExportPipeline{}
	for _, record := range repository.plans {
		plans = append(plans, clonePlanRecord(record))
	}
	for _, team := range repository.teams {
		teams = append(teams, cloneJSON(team))
	}
	for _, pipeline := range repository.exportPipelines {
		pipelines = append(pipelines, cloneJSON(pipeline))
	}
	if err := validateDefinitionReferences(definition, labelReferences(definition.ID, plans, teams, pipelines)); err != nil {
		return LabelDefinition{}, err
	}
	repository.labelDefinitions[definition.ID] = cloneJSON(definition)
	if repository.labelDefinitionRevisions[definition.ID] == nil {
		repository.labelDefinitionRevisions[definition.ID] = map[uint64]LabelDefinition{}
	}
	repository.labelDefinitionRevisions[definition.ID][definition.Revision] = cloneJSON(definition)
	return cloneJSON(definition), nil
}

func (repository *MemoryRepository) GetLabelDefinition(_ context.Context, id string) (LabelDefinition, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	definition, ok := repository.labelDefinitions[id]
	if !ok {
		return LabelDefinition{}, ErrNotFound
	}
	return cloneJSON(definition), nil
}

func (repository *MemoryRepository) ListLabelDefinitions(_ context.Context, includeTombstoned bool) ([]LabelDefinition, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := []LabelDefinition{}
	for _, definition := range repository.labelDefinitions {
		if includeTombstoned || definition.Status != LabelDefinitionTombstoned {
			result = append(result, cloneJSON(definition))
		}
	}
	sortLabelDefinitions(result)
	return result, nil
}

func (repository *MemoryRepository) ListLabelDefinitionRevisions(_ context.Context, id string) ([]LabelDefinition, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := []LabelDefinition{}
	for _, definition := range repository.labelDefinitionRevisions[id] {
		result = append(result, cloneJSON(definition))
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	sortLabelDefinitions(result)
	return result, nil
}

func (repository *FileRepository) PutLabelDefinition(_ context.Context, definition LabelDefinition) (LabelDefinition, error) {
	if err := validateLabelDefinition(definition, true); err != nil {
		return LabelDefinition{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	existing, err := repository.readLabelDefinition(definition.ID)
	if (errors.Is(err, ErrNotFound) && definition.Revision != 1) ||
		(err == nil && (definition.Revision != existing.Revision+1 || existing.Status == LabelDefinitionTombstoned)) {
		return LabelDefinition{}, ErrConflict
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return LabelDefinition{}, err
	}
	plans, err := repository.listPlansUnlocked("")
	if err != nil {
		return LabelDefinition{}, err
	}
	teams, err := repository.listTeamsUnlocked()
	if err != nil {
		return LabelDefinition{}, err
	}
	pipelines, err := repository.listExportPipelinesUnlocked()
	if err != nil {
		return LabelDefinition{}, err
	}
	if err := validateDefinitionReferences(definition, labelReferences(definition.ID, plans, teams, pipelines)); err != nil {
		return LabelDefinition{}, err
	}
	if err := ensureDirectory(filepath.Dir(repository.labelDefinitionRevisionPath(definition.ID, definition.Revision))); err != nil {
		return LabelDefinition{}, err
	}
	if err := atomicWriteJSON(repository.labelDefinitionRevisionPath(definition.ID, definition.Revision), definition); err != nil {
		return LabelDefinition{}, err
	}
	if err := atomicWriteJSON(repository.labelDefinitionPath(definition.ID), definition); err != nil {
		return LabelDefinition{}, err
	}
	return definition, nil
}

func (repository *FileRepository) GetLabelDefinition(_ context.Context, id string) (LabelDefinition, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.readLabelDefinition(id)
}

func (repository *FileRepository) ListLabelDefinitions(_ context.Context, includeTombstoned bool) ([]LabelDefinition, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listLabelDefinitionsUnlocked(includeTombstoned)
}

func (repository *FileRepository) ListLabelDefinitionRevisions(_ context.Context, id string) ([]LabelDefinition, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if !labelIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	entries, err := os.ReadDir(filepath.Join(repository.labelDefinitionRevisionDirectory(), id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := []LabelDefinition{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var definition LabelDefinition
		if err := readJSON(filepath.Join(repository.labelDefinitionRevisionDirectory(), id, entry.Name()), &definition); err != nil {
			return nil, err
		}
		result = append(result, definition)
	}
	sortLabelDefinitions(result)
	return result, nil
}

func (repository *FileRepository) readLabelDefinition(id string) (LabelDefinition, error) {
	if !labelIDPattern.MatchString(id) {
		return LabelDefinition{}, ErrNotFound
	}
	var definition LabelDefinition
	if err := readJSON(repository.labelDefinitionPath(id), &definition); err != nil {
		return LabelDefinition{}, err
	}
	if definition.ID != id || validateLabelDefinition(definition, true) != nil {
		return LabelDefinition{}, ErrConflict
	}
	return definition, nil
}

func (repository *FileRepository) listLabelDefinitionsUnlocked(includeTombstoned bool) ([]LabelDefinition, error) {
	entries, err := os.ReadDir(repository.labelDefinitionDirectory())
	if err != nil {
		return nil, err
	}
	result := []LabelDefinition{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var definition LabelDefinition
		if err := readJSON(filepath.Join(repository.labelDefinitionDirectory(), entry.Name()), &definition); err != nil {
			return nil, err
		}
		if includeTombstoned || definition.Status != LabelDefinitionTombstoned {
			result = append(result, definition)
		}
	}
	sortLabelDefinitions(result)
	return result, nil
}
