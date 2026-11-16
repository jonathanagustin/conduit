package devserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"time"
)

// reservePorts keeps both listeners live until launch. On randomized ephemeral
// allocators the accepted socket is closed first, leaving TIME_WAIT protection
// against later bind(:0) reuse. Explicit Go binds use SO_REUSEADDR. This follows
// the pinned CLI's internal allocator, rather than competing with it.
func reservePorts(ctx context.Context, count int) ([]*net.TCPListener, error) {
	listeners := make([]*net.TCPListener, 0, count)
	fail := func(err error) ([]*net.TCPListener, error) { closePorts(listeners); return nil, err }
	for range count {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			return fail(err)
		}
		listeners = append(listeners, listener)
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			continue
		}
		if err := pinPort(ctx, listener); err != nil {
			return fail(err)
		}
	}
	return listeners, nil
}

func pinPort(ctx context.Context, listener *net.TCPListener) error {
	deadline := time.Now().Add(time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := listener.SetDeadline(deadline); err != nil {
		return err
	}
	dialer := net.Dialer{Deadline: deadline}
	peer, err := dialer.DialContext(ctx, "tcp4", listener.Addr().String())
	if err != nil {
		return err
	}
	defer peer.Close()
	accepted, err := listener.AcceptTCP()
	if err != nil {
		return err
	}
	if err := accepted.Close(); err != nil {
		return err
	}
	if err := peer.SetReadDeadline(deadline); err != nil {
		return err
	}
	var probe [1]byte
	if _, err := peer.Read(probe[:]); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected data during loopback port reservation")
		}
		return fmt.Errorf("complete loopback port reservation: %w", err)
	}
	return nil
}

func closePorts(listeners []*net.TCPListener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
