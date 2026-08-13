#include "platform_cpsat_bridge.h"

#include <cstdlib>
#include <limits>
#include <string>

#include "ortools/sat/c_api/cp_solver_c.h"
#include "ortools/sat/cp_model.pb.h"
#include "ortools/sat/sat_parameters.pb.h"

extern "C" {

void* PlatformCpSatNewEnv() {
  // The official C API owns the environment allocation. Its raw new may throw
  // on allocation failure, so translate that failure to the C ABI's null path.
  // Each Rust request creates exactly one environment and never reuses it.
  try {
    return SolveCpNewEnv();
  } catch (...) {
    return nullptr;
  }
}

void PlatformCpSatDestroyEnv(void* environment) {
  if (environment == nullptr) return;
  SolveCpDestroyEnv(environment);
}

void PlatformCpSatStopSearch(void* environment) {
  if (environment == nullptr) return;
  SolveCpStopSearch(environment);
}

int PlatformCpSatSolve(void* environment, const uint8_t* model,
                       size_t model_length, const uint8_t* parameters,
                       size_t parameters_length,
                       size_t maximum_response_length, uint8_t** response,
                       size_t* response_length) {
  if (response == nullptr || response_length == nullptr) return 1;
  *response = nullptr;
  *response_length = 0;
  if (environment == nullptr || model == nullptr || parameters == nullptr ||
      model_length == 0 || parameters_length == 0 ||
      model_length > static_cast<size_t>(std::numeric_limits<int>::max()) ||
      parameters_length >
          static_cast<size_t>(std::numeric_limits<int>::max()) ||
      maximum_response_length == 0) {
    return 1;
  }

  try {
    // The official C API aborts on malformed protobuf through CHECK. Parse and
    // reserialize both messages first so only known-valid bytes reach it.
    operations_research::sat::CpModelProto parsed_model;
    operations_research::sat::SatParameters parsed_parameters;
    if (!parsed_model.ParseFromArray(model, static_cast<int>(model_length)) ||
        !parsed_parameters.ParseFromArray(
            parameters, static_cast<int>(parameters_length))) {
      return 2;
    }
    std::string validated_model;
    std::string validated_parameters;
    if (!parsed_model.SerializeToString(&validated_model) ||
        !parsed_parameters.SerializeToString(&validated_parameters) ||
        validated_model.empty() || validated_parameters.empty() ||
        validated_model.size() >
            static_cast<size_t>(std::numeric_limits<int>::max()) ||
        validated_parameters.size() >
            static_cast<size_t>(std::numeric_limits<int>::max())) {
      return 3;
    }

    void* solved = nullptr;
    int solved_length = 0;
    SolveCpInterruptible(
        environment, validated_model.data(),
        static_cast<int>(validated_model.size()), validated_parameters.data(),
        static_cast<int>(validated_parameters.size()), &solved, &solved_length);
    if (solved == nullptr || solved_length <= 0 ||
        static_cast<size_t>(solved_length) > maximum_response_length) {
      std::free(solved);
      return 4;
    }
    *response = static_cast<uint8_t*>(solved);
    *response_length = static_cast<size_t>(solved_length);
    return 0;
  } catch (...) {
    return 5;
  }
}

void PlatformCpSatFreeResponse(uint8_t* response) { std::free(response); }

}  // extern "C"
