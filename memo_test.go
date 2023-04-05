package memo

import (
	"context"
	"io"
	"syscall"
	"testing"
	"time"
)

type rws struct {
	buf []byte // inner buffer
	i   int    // buffer index
}

func (b *rws) Read(d []byte) (n int, err error) {
	n = copy(d, b.buf[b.i:])
	b.i += n
	if b.i >= len(b.buf) {
		err = io.EOF
	}
	return
}

func (b *rws) Write(s []byte) (int, error) {
	ni := b.i + len(s) // new index
	if ni >= len(b.buf) {
		nb := make([]byte, ni) // new buffer
		copy(nb, b.buf)
		copy(nb[b.i:], s)
		b.buf = nb
	}
	b.i = ni
	return len(s), nil
}

func (b *rws) Seek(offset int64, whence int) (int64, error) {
	if offset > int64(^uint(0)>>1) {
		return 0, syscall.EOVERFLOW
	}

	switch whence {
	case 0: // start
	case 1: // cur
		offset = int64(b.i) + offset
	case 2: // end
		offset = int64(len(b.buf)) + offset
	default:
		return 0, syscall.EINVAL
	}

	if offset < 0 {
		return 0, syscall.EINVAL
	} else if offset > int64(len(b.buf)) {
		return 0, syscall.ENXIO
	}

	b.i = int(offset)

	return offset, nil
}

func TestSerializedMemo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := map[string]int{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	file := rws{}

	{
		fn, _ := SerializedMemo(ctx, &file, func(k string) (int, error) {
			return m[k], nil
		})

		wanted := 1
		got, _ := fn("a")
		if got != 1 {
			t.Errorf(`wanted: %d, got: %d`, wanted, got)
		}

		cancel()
		_, _ = fn("z") // run once to prime the pump
	}

	// sleep is necessary to ensure context is cancelled
	time.Sleep(time.Millisecond * 1)
	m["a"] = 500
	file.i = 0

	{
		fn, _ := SerializedMemo(context.TODO(), &file, func(k string) (int, error) {
			return m[k], nil
		})

		wanted := 1
		got, _ := fn("a")
		if got != 1 {
			t.Errorf(`wanted: %d, got: %d`, wanted, got)
		}
	}
}
