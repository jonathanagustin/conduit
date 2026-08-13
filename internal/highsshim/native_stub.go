//go:build !highs || !cgo

package highsshim

import (
	"errors"
	"time"
)

type unavailableEngine struct{}

func (unavailableEngine) Solve(MatrixModel, time.Duration) (EngineResult, error) {
	return EngineResult{}, errors.New("HiGHS engine was not compiled into this binary")
}

// NewEngine returns the native engine only in an explicit highs+cgo build.
// Ordinary repository tests and binaries never acquire an implicit SDK link.
func NewEngine() Engine { return unavailableEngine{} }
