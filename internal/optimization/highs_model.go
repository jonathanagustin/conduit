package optimization

import (
	"encoding/json"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

const (
	HiGHSCompiledModelAPIVersion       = solverprotocol.HiGHSCompiledModelAPIVersion
	minimumHiGHSModelCoefficient       = solverprotocol.MinimumHiGHSModelCoefficient
	maximumHiGHSExactInteger     int64 = solverprotocol.MaximumHiGHSExactInteger
)

type HiGHSColumnKind = solverprotocol.HiGHSColumnKind

const (
	HiGHSBinaryColumn     = solverprotocol.HiGHSBinaryColumn
	HiGHSContinuousColumn = solverprotocol.HiGHSContinuousColumn
)

type HiGHSCompiledModel = solverprotocol.HiGHSCompiledModel
type HiGHSColumn = solverprotocol.HiGHSColumn
type HiGHSRow = solverprotocol.HiGHSRow
type HiGHSCoefficient = solverprotocol.HiGHSCoefficient
type HiGHSObjective = solverprotocol.HiGHSObjective
type HiGHSCostNormalization = solverprotocol.HiGHSCostNormalization
type HiGHSCostBase = solverprotocol.HiGHSCostBase

// CompileHiGHSModel preserves the parent package API while delegating the
// actual compiler and preflight to the transport-free solverprotocol package.
// JSON conversion is intentional: it exercises the exact wire boundary used
// between the host and the stdin/stdout shim without sharing parent internals.
func CompileHiGHSModel(request SolverRequest) (HiGHSCompiledModel, error) {
	protocolRequest, err := solverProtocolRequest(request)
	if err != nil {
		return HiGHSCompiledModel{}, err
	}
	return solverprotocol.CompileHiGHSModel(protocolRequest)
}

func solverProtocolRequest(request SolverRequest) (solverprotocol.SolverRequest, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return solverprotocol.SolverRequest{}, fmt.Errorf("encode solver protocol request: %w", err)
	}
	var protocolRequest solverprotocol.SolverRequest
	if err := json.Unmarshal(encoded, &protocolRequest); err != nil {
		return solverprotocol.SolverRequest{}, fmt.Errorf("convert solver protocol request: %w", err)
	}
	return protocolRequest, nil
}
