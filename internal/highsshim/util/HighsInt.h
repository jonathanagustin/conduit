#ifndef PLATFORM_INTERNAL_HIGHSSHIM_UTIL_HIGHSINT_H_
#define PLATFORM_INTERNAL_HIGHSSHIM_UTIL_HIGHSINT_H_

// HiGHS' installed HConst.h uses the source-tree-relative spelling
// "util/HighsInt.h". The official SDK exposes it below the public highs/
// prefix, so this forwarding header preserves the exact upstream declaration
// without copying or re-declaring the ABI type.
#include "highs/util/HighsInt.h"

#endif  // PLATFORM_INTERNAL_HIGHSSHIM_UTIL_HIGHSINT_H_
