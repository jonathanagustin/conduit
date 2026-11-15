//go:build !linux

package filesystem

import (
	"context"
	"errors"
)

type Reader struct{ Path string }

func (Reader) Capacity(context.Context) (int64, int64, error) {
	return 0, 0, errors.New("filesystem telemetry unavailable on this platform")
}
