//go:build highs && cgo

package highsshim

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lhighs -lhighs_extras -lz -lm -ldl -lpthread -lstdc++
#include <stdint.h>
#include "bridge.h"

static int32_t platform_highs_solve_flat(
    int32_t num_col, int32_t num_row, int32_t num_nz,
    const double* col_cost, const double* col_lower, const double* col_upper,
    const double* row_lower, const double* row_upper,
    const int32_t* a_start, const int32_t* a_index, const double* a_value,
    const int32_t* integrality, double time_limit_seconds,
    double* col_value, PlatformHighsResult* result) {
  PlatformHighsModel model = {
    num_col, num_row, num_nz, col_cost, col_lower, col_upper,
    row_lower, row_upper, a_start, a_index, a_value, integrality
  };
  return platform_highs_solve(&model, time_limit_seconds, col_value, result);
}
*/
import "C"

import (
	"errors"
	"math"
	"runtime"
	"time"
	"unsafe"
)

type nativeEngine struct{}

func NewEngine() Engine { return nativeEngine{} }

func (nativeEngine) Solve(model MatrixModel, limit time.Duration) (EngineResult, error) {
	if limit < 0 || len(model.ColumnCost) == 0 || len(model.ColumnLower) != len(model.ColumnCost) ||
		len(model.ColumnUpper) != len(model.ColumnCost) || len(model.Integrality) != len(model.ColumnCost) ||
		len(model.RowLower) != len(model.RowUpper) || len(model.ColumnStarts) != len(model.ColumnCost)+1 ||
		len(model.RowIndices) != len(model.Values) {
		return EngineResult{}, errors.New("invalid native HiGHS matrix")
	}
	if len(model.ColumnCost) > math.MaxInt32 || len(model.RowLower) > math.MaxInt32 || len(model.Values) > math.MaxInt32 {
		return EngineResult{}, errors.New("native HiGHS matrix exceeds ABI limits")
	}
	columns := make([]float64, len(model.ColumnCost))
	var raw C.PlatformHighsResult
	status := C.platform_highs_solve_flat(
		C.int32_t(len(model.ColumnCost)), C.int32_t(len(model.RowLower)), C.int32_t(len(model.Values)),
		doublePointer(model.ColumnCost), doublePointer(model.ColumnLower), doublePointer(model.ColumnUpper),
		doublePointer(model.RowLower), doublePointer(model.RowUpper), int32Pointer(model.ColumnStarts),
		int32Pointer(model.RowIndices), doublePointer(model.Values), int32Pointer(model.Integrality),
		C.double(limit.Seconds()), doublePointer(columns), &raw,
	)
	runtime.KeepAlive(model)
	runtime.KeepAlive(columns)
	if status < 0 || raw.call_status < 0 {
		return EngineResult{}, errors.New("native HiGHS API call failed")
	}
	result := EngineResult{
		ModelStatus: int(raw.model_status), PrimalSolutionStatus: int(raw.primal_solution_status),
		ObjectiveValue: float64(raw.objective_value), BestBound: float64(raw.best_bound), Gap: float64(raw.gap),
	}
	if result.PrimalSolutionStatus == primalFeasibleStatus {
		result.Columns = columns
	}
	return result, nil
}

func doublePointer(values []float64) *C.double {
	if len(values) == 0 {
		return nil
	}
	return (*C.double)(unsafe.Pointer(&values[0]))
}

func int32Pointer(values []int32) *C.int32_t {
	if len(values) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&values[0]))
}
