package container

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

// Start runs the workspace command in a non-root container on a dedicated
// internal network. The forwarder is the only path to the configured git host
// and registry. A missing runtime binary or network does not fall back to a
// host process.
func (runtime *Runtime) Start(ctx context.Context, workspace domain.Workspace) (domain.Runtime, error) {
	if err := ctx.Err(); err != nil {
		return domain.Runtime{}, err
	}
	runtime.mu.Lock()
	if runtime.runs == nil {
		runtime.runs = map[string]*session{}
	}
	if current, ok := runtime.runs[workspace.ID]; ok {
		current.mu.Lock()
		status := current.status
		result := current.view(workspace.ID)
		current.mu.Unlock()
		if status == domain.StatusRunning || status == domain.StatusStarting {
			runtime.mu.Unlock()
			return result, nil
		}
	}
	runtime.mu.Unlock()

	validated, err := domain.NormalizeAndValidateEgress(runtime.Egress)
	if err != nil {
		return domain.Runtime{}, err
	}
	image, err := runtime.image(ctx)
	if err != nil {
		return failed(workspace.ID, err), err
	}
	box, err := newSandbox(runtime.Bin)
	if err != nil {
		return failed(workspace.ID, err), err
	}
	started := false
	defer func() {
		if !started {
			box.cleanup()
		}
	}()
	if err := box.createNetworks(ctx); err != nil {
		return failed(workspace.ID, err), fmt.Errorf("%w: %v", domain.ErrDisabled, err)
	}
	rules := []forwardRule{
		{Listen: validated.GitPort, Upstream: netJoin(validated.GitHost, validated.GitPort)},
		{Listen: validated.RegistryPort, Upstream: netJoin(validated.Registry, validated.RegistryPort)},
	}
	if validated.GitPort == validated.RegistryPort {
		err := fmt.Errorf("%w: git and registry ports must differ", domain.ErrInvalidEgress)
		return failed(workspace.ID, err), err
	}
	if err := box.startForwarder(ctx, image, rules); err != nil {
		return failed(workspace.ID, err), fmt.Errorf("%w: %v", domain.ErrDisabled, err)
	}
	if err := box.attachInternal(ctx); err != nil {
		return failed(workspace.ID, err), fmt.Errorf("%w: %v", domain.ErrDisabled, err)
	}
	forwarderIP, err := box.ip(ctx, box.forwarder, box.internalNet)
	if err != nil {
		return failed(workspace.ID, err), err
	}
	name := box.name("ws")
	args, err := domain.ContainerRunArgs(domain.LaunchRequest{
		Image:   image,
		Name:    name,
		Network: box.internalNet,
		Command: []string{"sleep"},
		Detach:  true,
		ExtraHosts: []string{
			validated.GitHost + ":" + forwarderIP,
			validated.Registry + ":" + forwarderIP,
		},
	})
	if err != nil {
		return failed(workspace.ID, err), err
	}
	if _, _, err := box.cli.run(ctx, args...); err != nil {
		return failed(workspace.ID, err), fmt.Errorf("%w: %v", domain.ErrDisabled, err)
	}
	box.names = append(box.names, name)
	if err := box.waitRunning(ctx, name); err != nil {
		return failed(workspace.ID, err), fmt.Errorf("%w: %v", domain.ErrDisabled, err)
	}
	entry := &session{status: domain.StatusRunning, box: box}
	runtime.mu.Lock()
	runtime.runs[workspace.ID] = entry
	runtime.mu.Unlock()
	started = true
	return entry.view(workspace.ID), nil
}

func (runtime *Runtime) Stop(ctx context.Context, workspaceID string) (domain.Runtime, error) {
	if err := ctx.Err(); err != nil {
		return domain.Runtime{}, err
	}
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.Runtime{WorkspaceID: workspaceID, Status: domain.StatusStopped, Origin: domain.OriginLocalRuntime}, nil
	}
	entry.mu.Lock()
	box := entry.box
	entry.status = domain.StatusStopped
	entry.box = nil
	entry.mu.Unlock()
	if box != nil {
		box.cleanup()
	}
	return domain.Runtime{ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Status: domain.StatusStopped, Origin: domain.OriginLocalRuntime}, nil
}

func (runtime *Runtime) Status(_ context.Context, workspaceID string) (domain.Runtime, error) {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.Runtime{}, domain.ErrNotFound
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.view(workspaceID), nil
}

func (runtime *Runtime) Logs(_ context.Context, workspaceID, cursor string, limit int) (domain.LogPage, error) {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.LogPage{}, domain.ErrNotFound
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	start := 0
	if cursor != "" {
		for index, line := range entry.logs {
			if line == cursor {
				start = index + 1
			}
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	lines := append([]string{}, entry.logs[start:]...)
	next := ""
	if len(lines) > limit {
		lines = lines[:limit]
		next = lines[len(lines)-1]
	}
	return domain.LogPage{Lines: lines, NextCursor: next, Origin: domain.OriginLocalRuntime, Status: entry.status, ValueTier: domain.ValueTier}, nil
}

func (runtime *Runtime) Clear(_ context.Context, workspaceID string) error {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return nil
	}
	entry.mu.Lock()
	entry.logs = nil
	entry.mu.Unlock()
	return nil
}

func (entry *session) view(workspaceID string) domain.Runtime {
	return domain.Runtime{
		ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Status: entry.status,
		Failure: entry.failure, Origin: domain.OriginLocalRuntime,
	}
}

func failed(workspaceID string, err error) domain.Runtime {
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return domain.Runtime{ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Status: domain.StatusFailed, Failure: reason, Origin: domain.OriginLocalRuntime}
}
