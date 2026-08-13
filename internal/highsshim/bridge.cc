//go:build highs && cgo

#include "bridge.h"

#include <cmath>
#include <cstring>

#include "highs/interfaces/highs_c_api.h"
#include "highs/lp_data/HConst.h"

static_assert(sizeof(HighsInt) == sizeof(int32_t),
              "Platform's pinned HiGHS ABI requires 32-bit HighsInt");
static_assert(static_cast<int>(HighsModelStatus::kMemoryLimit) == 18,
              "HiGHS 1.15.1 memory-limit status changed");
static_assert(static_cast<int>(HighsModelStatus::kHighsInterrupt) == 19,
              "HiGHS 1.15.1 internal-interrupt status changed");

namespace {

bool ok(HighsInt status) { return status == kHighsStatusOk; }

// Deliberately enumerate the complete typed 1.15.1 status surface. The C API
// header stops naming values at 17 even though Highs_getModelStatus can return
// the two later C++ values.
bool known_model_status(HighsInt raw) {
  switch (static_cast<HighsModelStatus>(raw)) {
    case HighsModelStatus::kNotset:
    case HighsModelStatus::kLoadError:
    case HighsModelStatus::kModelError:
    case HighsModelStatus::kPresolveError:
    case HighsModelStatus::kSolveError:
    case HighsModelStatus::kPostsolveError:
    case HighsModelStatus::kModelEmpty:
    case HighsModelStatus::kOptimal:
    case HighsModelStatus::kInfeasible:
    case HighsModelStatus::kUnboundedOrInfeasible:
    case HighsModelStatus::kUnbounded:
    case HighsModelStatus::kObjectiveBound:
    case HighsModelStatus::kObjectiveTarget:
    case HighsModelStatus::kTimeLimit:
    case HighsModelStatus::kIterationLimit:
    case HighsModelStatus::kUnknown:
    case HighsModelStatus::kSolutionLimit:
    case HighsModelStatus::kInterrupt:
    case HighsModelStatus::kMemoryLimit:
    case HighsModelStatus::kHighsInterrupt:
      return true;
  }
  return false;
}

}  // namespace

extern "C" int32_t platform_highs_solve(const PlatformHighsModel* model,
                                          double time_limit_seconds,
                                          double* col_value,
                                          PlatformHighsResult* result) {
  if (model == nullptr || result == nullptr || model->num_col < 0 ||
      model->num_row < 0 || model->num_nz < 0 ||
      !std::isfinite(time_limit_seconds) || time_limit_seconds < 0) {
    return kHighsStatusError;
  }
  std::memset(result, 0, sizeof(*result));
  result->model_status = -1;
  result->primal_solution_status = kSolutionStatusNone;
  result->objective_value = INFINITY;
  result->best_bound = -INFINITY;
  result->gap = INFINITY;

  if (std::strcmp(Highs_version(), "1.15.1") != 0 ||
      std::strncmp(Highs_githash(), "04024d7", 7) != 0) {
    result->call_status = kHighsStatusError;
    return kHighsStatusError;
  }

  void* highs = Highs_create();
  if (highs == nullptr) {
    result->call_status = kHighsStatusError;
    return kHighsStatusError;
  }

  HighsInt status = kHighsStatusOk;
  auto set_status = [&](HighsInt next) {
    if (status == kHighsStatusOk && next != kHighsStatusOk) status = next;
  };
  set_status(Highs_setBoolOptionValue(highs, "output_flag", 0));
  set_status(Highs_setStringOptionValue(highs, "parallel", "off"));
  set_status(Highs_setIntOptionValue(highs, "threads", 1));
  set_status(Highs_setIntOptionValue(highs, "random_seed", 0));
  set_status(Highs_setDoubleOptionValue(highs, "mip_rel_gap", 0));
  set_status(Highs_setDoubleOptionValue(highs, "mip_abs_gap", 0));
  set_status(Highs_setDoubleOptionValue(highs, "primal_feasibility_tolerance",
                                        1e-10));
  set_status(Highs_setDoubleOptionValue(highs, "mip_feasibility_tolerance",
                                        1e-10));
  set_status(Highs_setDoubleOptionValue(highs, "small_matrix_value", 1e-12));
  set_status(Highs_setDoubleOptionValue(highs, "time_limit",
                                        time_limit_seconds));
  if (ok(status)) {
    set_status(Highs_passMip(
        highs, model->num_col, model->num_row, model->num_nz,
        kHighsMatrixFormatColwise, kHighsObjSenseMinimize, 0,
        model->col_cost, model->col_lower, model->col_upper, model->row_lower,
        model->row_upper, reinterpret_cast<const HighsInt*>(model->a_start),
        reinterpret_cast<const HighsInt*>(model->a_index), model->a_value,
        reinterpret_cast<const HighsInt*>(model->integrality)));
  }
  if (ok(status)) {
    const HighsInt run_status = Highs_run(highs);
    // A warning still carries a model status (including resource and
    // interrupt stops) that the protocol must classify explicitly.
    status = run_status;
  }

  result->call_status = status;
  result->model_status = Highs_getModelStatus(highs);
  if (!known_model_status(result->model_status)) status = kHighsStatusError;

  HighsInt primal_status = kSolutionStatusNone;
  if (!ok(Highs_getIntInfoValue(highs, "primal_solution_status",
                                &primal_status))) {
    primal_status = kSolutionStatusNone;
  }
  result->primal_solution_status = primal_status;
  if (primal_status == kSolutionStatusFeasible) {
    if (col_value == nullptr ||
        !ok(Highs_getSolution(highs, col_value, nullptr, nullptr, nullptr))) {
      status = kHighsStatusError;
      result->primal_solution_status = kSolutionStatusNone;
    } else {
      result->objective_value = Highs_getObjectiveValue(highs);
      double value = 0;
      if (ok(Highs_getDoubleInfoValue(highs, "mip_dual_bound", &value)))
        result->best_bound = value;
      if (ok(Highs_getDoubleInfoValue(highs, "mip_gap", &value)))
        result->gap = value;
    }
  }

  result->call_status = status;
  Highs_destroy(highs);
  return status;
}
