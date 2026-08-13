package optimization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

const (
	ExternalSolverProcessAPIVersion = solverprotocol.ExternalSolverProcessAPIVersion
	defaultMaximumSolverOutput      = 8 << 20
	maximumSolverExecutableBytes    = 512 << 20
)

// ExternalSolverAssignmentChoice is the complete provider-authored solution
// surface. Demand, cost, relaxation, and aggregate metrics remain owned by the
// Platform host and are reconstructed from the bound SolverRequest.
type ExternalSolverAssignmentChoice = solverprotocol.ExternalSolverAssignmentChoice

// ExternalSolverProcessResponse is the deliberately small stdout protocol for
// a digest-pinned solver artifact. The artifact never authors its descriptor:
// the caller binds that to verified executable bytes before execution. A
// pointer preserves the distinction between no incumbent and an explicitly
// empty incumbent for an empty problem.
type ExternalSolverProcessResponse = solverprotocol.ExternalSolverProcessResponse

// ExternalProcessSolverBackend is a hexagonal adapter for a separately built
// solver artifact. It executes no shell, inherits no environment, copies the
// configured regular file into a private staging directory while hashing it,
// and admits stdout only through ExternalSolverProcessResponse. The staged file
// is hashed again after execution. Invocation configuration must be compiled
// into that executable or a digest-pinned wrapper; accepting unrecorded argv
// would let behavior change without changing the receipt. Network isolation,
// process-tree containment, dependency-closure admission, and hard memory
// limits are distribution/runtime responsibilities and remain separate gates.
type ExternalProcessSolverBackend struct {
	BackendDescriptor  SolverBackendDescriptor
	ExecutablePath     string
	StagingDirectory   string
	MaximumOutputBytes int
}

func (backend ExternalProcessSolverBackend) Descriptor() SolverBackendDescriptor {
	return canonicalBackendDescriptor(backend.BackendDescriptor)
}

func (backend ExternalProcessSolverBackend) Solve(ctx context.Context, request SolverRequest) (SolverBackendOutcome, error) {
	if err := VerifySolverRequest(request); err != nil {
		return SolverBackendOutcome{}, err
	}
	descriptor := backend.Descriptor()
	if err := validateBackendDescriptor(descriptor); err != nil {
		return SolverBackendOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return SolverBackendOutcome{Termination: SolverTimeLimit, Explanation: "external solver time limit elapsed before execution"}, nil
	}
	if err := validateExternalSolverProcessConfiguration(backend); err != nil {
		return SolverBackendOutcome{}, err
	}
	staged, cleanup, stageErr := backend.stageVerifiedExecutable()
	if stageErr != nil {
		if errors.Is(stageErr, os.ErrNotExist) {
			return SolverBackendOutcome{}, ErrSolverBackendUnavailable
		}
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver executable failed integrity admission"}, nil
	}
	defer cleanup()

	input, err := json.Marshal(request)
	if err != nil {
		return SolverBackendOutcome{}, fmt.Errorf("encode external solver request: %w", err)
	}
	maximumOutput := backend.MaximumOutputBytes
	if maximumOutput == 0 {
		maximumOutput = defaultMaximumSolverOutput
	}
	stdout := &boundedProcessBuffer{maximum: maximumOutput}
	stderr := &boundedProcessBuffer{maximum: maximumSolverExplanation}
	command := exec.CommandContext(ctx, staged)
	command.Dir = filepath.Dir(staged)
	command.Env = []string{"LANG=C", "LC_ALL=C", "TZ=UTC"}
	command.Stdin = bytes.NewReader(input)
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 250 * time.Millisecond
	runErr := command.Run()
	if ctx.Err() != nil {
		return SolverBackendOutcome{Termination: SolverTimeLimit, Explanation: "external solver exceeded its time limit"}, nil
	}
	if integrityErr := verifyFileDigest(staged, descriptor.ExecutableDigest); integrityErr != nil {
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver staged executable changed during execution"}, nil
	}
	if errors.Is(stdout.err, errBoundedProcessOutput) || errors.Is(stderr.err, errBoundedProcessOutput) {
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver exceeded its bounded output contract"}, nil
	}
	if runErr != nil {
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver process failed"}, nil
	}
	response, err := decodeExternalSolverProcessResponse(stdout.Bytes())
	if err != nil {
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver emitted an invalid response envelope"}, nil
	}
	if response.RequestID != request.ID || response.ModelDigest != request.Model.Digest {
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver response does not bind the request and model"}, nil
	}
	var result *Result
	if response.Assignments != nil {
		reconstructed, reconstructErr := reconstructExternalSolverResult(request.Recommendation, *response.Assignments)
		if reconstructErr != nil {
			return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver emitted invalid assignment choices"}, nil
		}
		result = &reconstructed
	}
	outcome := SolverBackendOutcome{
		Termination: response.Termination, BackendReportedEvidence: cloneBackendReportedEvidence(response.BackendReportedEvidence),
		Result: result, Explanation: boundedSolverExplanation(response.Explanation),
	}
	if err := validateExternalSolverOutcomeShape(request, outcome); err != nil {
		return SolverBackendOutcome{Termination: SolverError, Explanation: "external solver emitted inconsistent termination evidence"}, nil
	}
	return outcome, nil
}

func reconstructExternalSolverResult(recommendation RecommendationRequest, choices []ExternalSolverAssignmentChoice) (Result, error) {
	items, resources, eligible, err := validate(recommendation.Problem)
	if err != nil {
		return Result{}, err
	}
	reference, err := solve(recommendation.Problem, hasObjective(recommendation.Objective.Secondary, "minimize_estimated_cost"))
	if err != nil {
		return Result{}, err
	}
	if len(choices) != len(items) {
		return Result{}, errors.New("external solver choices must cover every work item exactly once")
	}

	ordered := append([]ExternalSolverAssignmentChoice(nil), choices...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Item == ordered[j].Item {
			return ordered[i].Resource < ordered[j].Resource
		}
		return ordered[i].Item < ordered[j].Item
	})
	compilerChoices := make([]allocationChoice, 0, len(ordered))
	for _, choice := range ordered {
		compilerChoices = append(compilerChoices, allocationChoice{Item: choice.Item, Resource: choice.Resource})
	}
	return buildAllocationResult(
		items,
		resources,
		eligible,
		fractionMaps(reference.Fractions),
		reference.RelaxedMakespan,
		recommendation.Problem.CostModel,
		compilerChoices,
	)
}

func fractionMaps(fractions map[string][]Fraction) map[string]map[string]float64 {
	result := make(map[string]map[string]float64, len(fractions))
	for item, values := range fractions {
		result[item] = make(map[string]float64, len(values))
		for _, fraction := range values {
			result[item][fraction.Resource] = fraction.Share
		}
	}
	return result
}

func validateExternalSolverProcessConfiguration(backend ExternalProcessSolverBackend) error {
	if !filepath.IsAbs(backend.ExecutablePath) || backend.ExecutablePath == filepath.Clean(string(filepath.Separator)) {
		return errors.New("external solver executable path must be absolute and narrow")
	}
	if backend.StagingDirectory != "" && !filepath.IsAbs(backend.StagingDirectory) {
		return errors.New("external solver staging directory must be absolute")
	}
	if backend.MaximumOutputBytes < 0 || backend.MaximumOutputBytes > defaultMaximumSolverOutput {
		return fmt.Errorf("external solver output bound must be between 1 and %d bytes when set", defaultMaximumSolverOutput)
	}
	return nil
}

func (backend ExternalProcessSolverBackend) stageVerifiedExecutable() (string, func(), error) {
	info, err := os.Lstat(backend.ExecutablePath)
	if err != nil {
		return "", func() {}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximumSolverExecutableBytes {
		return "", func() {}, errors.New("external solver executable must be a bounded regular file, not a symlink")
	}
	source, err := os.Open(backend.ExecutablePath)
	if err != nil {
		return "", func() {}, err
	}
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || openedInfo.Size() < 1 || openedInfo.Size() > maximumSolverExecutableBytes {
		return "", func() {}, errors.New("external solver executable changed during admission")
	}
	staging, err := os.MkdirTemp(backend.StagingDirectory, "platform-solver-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(staging) }
	targetPath := filepath.Join(staging, "backend")
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o500)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(target, hash), io.LimitReader(source, maximumSolverExecutableBytes+1))
	syncErr := target.Sync()
	closeErr := target.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written < 1 || written > maximumSolverExecutableBytes {
		cleanup()
		return "", func() {}, errors.New("copy external solver executable")
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if digest != backend.BackendDescriptor.ExecutableDigest {
		cleanup()
		return "", func() {}, errors.New("external solver executable digest mismatch")
	}
	return targetPath, cleanup, nil
}

func verifyFileDigest(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maximumSolverExecutableBytes+1))
	if err != nil || written < 1 || written > maximumSolverExecutableBytes {
		return errors.New("hash external solver executable")
	}
	got := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return errors.New("external solver executable digest mismatch")
	}
	return nil
}

func decodeExternalSolverProcessResponse(encoded []byte) (ExternalSolverProcessResponse, error) {
	var response ExternalSolverProcessResponse
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return ExternalSolverProcessResponse{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ExternalSolverProcessResponse{}, errors.New("external solver response contains trailing JSON")
	}
	if response.APIVersion != ExternalSolverProcessAPIVersion || response.Kind != "ExternalSolverProcessResponse" || !validContentID(response.RequestID, "solreq_") || !validDigest(response.ModelDigest) {
		return ExternalSolverProcessResponse{}, errors.New("unsupported external solver response")
	}
	return response, nil
}

func validateExternalSolverOutcomeShape(request SolverRequest, outcome SolverBackendOutcome) error {
	valid := map[SolverTermination]bool{
		SolverOptimal: true, SolverFeasible: true, SolverInfeasible: true,
		SolverUnbounded: true, SolverTimeLimit: true, SolverError: true, SolverUnknown: true,
	}
	if !valid[outcome.Termination] {
		return errors.New("unknown external solver termination")
	}
	switch outcome.Termination {
	case SolverOptimal, SolverFeasible:
		if outcome.Result == nil {
			return errors.New("successful external solver outcome lacks a result")
		}
	case SolverTimeLimit:
		if outcome.Explanation == "" {
			return errors.New("time-limited external solver outcome lacks an explanation")
		}
	case SolverUnknown:
		if outcome.Result != nil || outcome.Explanation == "" {
			return errors.New("unknown external solver outcome has inconsistent evidence")
		}
	case SolverInfeasible, SolverUnbounded, SolverError:
		if outcome.Result != nil || outcome.Explanation == "" {
			return errors.New("terminal external solver outcome has inconsistent evidence")
		}
	}
	if err := verifyBackendReportedEvidence(request, outcome.BackendReportedEvidence, true, outcome.Result); err != nil {
		return err
	}
	if outcome.Termination == SolverOptimal {
		if err := verifyReportedOptimalProof(request, outcome.BackendReportedEvidence, outcome.Result); err != nil {
			return err
		}
	}
	return nil
}

var errBoundedProcessOutput = errors.New("bounded process output exceeded")

type boundedProcessBuffer struct {
	buffer  bytes.Buffer
	maximum int
	err     error
}

func (buffer *boundedProcessBuffer) Write(value []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	remaining := buffer.maximum - buffer.buffer.Len()
	if len(value) > remaining {
		if remaining > 0 {
			_, _ = buffer.buffer.Write(value[:remaining])
		}
		buffer.err = errBoundedProcessOutput
		return len(value), buffer.err
	}
	return buffer.buffer.Write(value)
}

func (buffer *boundedProcessBuffer) Bytes() []byte  { return buffer.buffer.Bytes() }
func (buffer *boundedProcessBuffer) String() string { return buffer.buffer.String() }
