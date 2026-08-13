#ifndef PLATFORM_INTERNAL_HIGHSSHIM_BRIDGE_H_
#define PLATFORM_INTERNAL_HIGHSSHIM_BRIDGE_H_

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct PlatformHighsModel {
  int32_t num_col;
  int32_t num_row;
  int32_t num_nz;
  const double* col_cost;
  const double* col_lower;
  const double* col_upper;
  const double* row_lower;
  const double* row_upper;
  const int32_t* a_start;
  const int32_t* a_index;
  const double* a_value;
  const int32_t* integrality;
} PlatformHighsModel;

typedef struct PlatformHighsResult {
  int32_t call_status;
  int32_t model_status;
  int32_t primal_solution_status;
  double objective_value;
  double best_bound;
  double gap;
} PlatformHighsResult;

// platform_highs_solve owns no memory passed by the caller. col_value must
// point to num_col doubles. It is populated only when HiGHS reports a feasible
// primal solution.
int32_t platform_highs_solve(const PlatformHighsModel* model,
                             double time_limit_seconds,
                             double* col_value,
                             PlatformHighsResult* result);

#ifdef __cplusplus
}
#endif

#endif  // PLATFORM_INTERNAL_HIGHSSHIM_BRIDGE_H_
