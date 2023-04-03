package memo

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"sync"
)

// parameter, result
type entry[K, V any] struct {
	Key   K
	Value V
}

// returns a memo fn with entries recorded to the ReadWriteSeeker
func SerializedMemo[K comparable, V any, F func(K) (V, error)](ctx context.Context, f io.ReadWriteSeeker, fn F) (F, error) {
	ctx, cancel := context.WithCancel(ctx)
	memo := make(map[K]V)

	dec := json.NewDecoder(f)
	for {
		var o int64 = 0
		var e entry[K, V]

		if err := dec.Decode(&e); err != nil {
			if err != io.EOF {
				// we will overwrite the file starting from before we saw the error
				_, err = f.Seek(o, os.SEEK_SET)
				if err != nil {
					return nil, err
				}
			}
			break
		}

		o = dec.InputOffset()
		memo[e.Key] = e.Value
	}

	var encErr error        // error from json encoder
	var ml, el sync.RWMutex // map lock & error lock

	entries := make(chan entry[K, V], runtime.NumCPU())
	// TODO: explain how this routine is garbage collected
	go func() {
		// assuming the decoder didn't adjust the file cursor,
		// we can pass this straight to the encoder
		enc := json.NewEncoder(f)
		for e := range entries {
			ml.Lock()
			memo[e.Key] = e.Value
			ml.Unlock()
			err := enc.Encode(e)
			if err != nil {
				el.Lock()
				encErr = err
				el.Unlock()
				cancel()
				return
			}
		}
	}()

	return func(k K) (V, error) {
		ml.RLock()
		v, ok := memo[k]
		ml.RUnlock()
		if ok {
			return v, nil
		}

		v, err := fn(k)
		if err != nil {
			return v, err
		}

		e := entry[K, V]{
			Key:   k,
			Value: v,
		}

		select {
		case <-ctx.Done():
			el.RLock()
			if encErr != nil {
				return v, encErr
			}
			el.RUnlock()
			return v, ctx.Err()
		case entries <- e:
		}

		return v, nil
	}, nil
}
