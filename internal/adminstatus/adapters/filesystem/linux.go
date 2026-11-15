//go:build linux

package filesystem

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"math"
)

type Reader struct{ Path string }

func (reader Reader) Capacity(ctx context.Context) (used, total int64, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	var stat unix.Statfs_t
	if err = unix.Statfs(reader.Path, &stat); err != nil {
		return
	}
	if stat.Bsize <= 0 || stat.Bavail > stat.Blocks || stat.Blocks > math.MaxInt64/uint64(stat.Bsize) {
		return 0, 0, errors.New("invalid filesystem capacity")
	}
	total = int64(stat.Blocks) * stat.Bsize
	used = int64(stat.Blocks-stat.Bavail) * stat.Bsize
	return
}
