package memo

import (
	"context"
	"encoding/json"
	"github.com/spf13/afero"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// parameter, result
type entry[K, V any] struct {
	Key   K
	Value V
}

// returns an fs-backed memo fn
func MemoFS[K comparable, V any, F func(K) (V, error)](ctx context.Context, fsys afero.Fs, path string, fn F) (F, error) {
	ctx, cancel := context.WithCancel(context.TODO())
	memo := make(map[K]V)

	// TODO: just take a file (or ReadWriteTruncaterSeeker) instead
	f, err := fsys.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			err = fsys.MkdirAll(filepath.Dir(path), 0755)
			if err != nil {
				return nil, err
			}
			f, err = fsys.OpenFile(path, os.O_RDWR|os.O_CREATE, 0755)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		dec := json.NewDecoder(f)
		for {
			var e entry[K, V]

			o := dec.InputOffset()

			err = dec.Decode(&e)
			if err != nil {
				if err == io.EOF {
					break
				} else {
					// we will overwrite the file starting from before we saw the error
					// TODO: assert that this assumption works
					_, err = f.Seek(o, os.SEEK_SET)
					if err != nil {
						return nil, err
					}
					err = f.Truncate(o)
					if err != nil {
						return nil, err
					}
					break
				}
			}
			memo[e.Key] = e.Value
		}
	}

	var encErr error        // error from json encoder
	var ml, el sync.RWMutex // map lock & error lock

	entries := make(chan entry[K, V], runtime.NumCPU())
	// TODO: explain how this routine is garbage collected
	go func(f afero.File) {
		// assuming the decoder didn't adjust the file cursor,
		// we can pass this straight to the encoder
		enc := json.NewEncoder(f)
		defer f.Close()
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
	}(f)

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
