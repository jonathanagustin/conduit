#ifndef PLATFORM_CPSAT_BRIDGE_H_
#define PLATFORM_CPSAT_BRIDGE_H_

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void* PlatformCpSatNewEnv(void);
void PlatformCpSatDestroyEnv(void* environment);
void PlatformCpSatStopSearch(void* environment);
int PlatformCpSatSolve(void* environment, const uint8_t* model,
                       size_t model_length, const uint8_t* parameters,
                       size_t parameters_length, size_t maximum_response_length,
                       uint8_t** response, size_t* response_length);
void PlatformCpSatFreeResponse(uint8_t* response);

#ifdef __cplusplus
}
#endif

#endif  // PLATFORM_CPSAT_BRIDGE_H_
