package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"sync"

	"github.com/grailbio/base/digest"
	"github.com/grailbio/reflow"
	"github.com/grailbio/reflow/syntax"
	"golang.org/x/sync/errgroup"
)

// Bundle represents a self contained reflow program. It contains all the necessary sources,
// arguments and image references to be able to reproduce the exact same reflow program invocation.
// The bundle assumes that the image references are assumed to remain unchanged.
type Bundle struct {
	// Name of the main program to be run.
	Name string
	// Args is the list of args to the program.
	Args []string
	// Inline is the inlined source bytes of the main and the dependent modules.
	Inline syntax.Inline `json:"-"`
	// Files is map of the paths to digest.
	Files map[string]digest.Digest
	// Images is the list of docker image names used in this program.
	Images []string
}

// ReadBundle retrieves a Bundle from the repository given a program's runId. This allows reproducing the exact same
// state (program, args and images) from a previous run.
func ReadBundle(ctx context.Context, d digest.Digest, repo reflow.Repository) (*Bundle, error) {
	r, err := repo.Get(ctx, d)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	bundle := Bundle{Inline: syntax.Inline{}}
	err = json.Unmarshal(b, &bundle)
	if err != nil {
		return nil, err
	}
	g, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	for k, v := range bundle.Files {
		k, v := k, v
		g.Go(func() error {
			r, err := repo.Get(ctx, v)
			if err != nil {
				return err
			}
			b, err := ioutil.ReadAll(r)
			if err != nil {
				return err
			}
			mu.Lock()
			bundle.Inline[k] = b
			mu.Unlock()
			return nil
		})
	}
	if err = g.Wait(); err != nil {
		return nil, err
	}
	return &bundle, nil
}

// Write writes the bundle to the repository.
func (p *Bundle) Write(ctx context.Context, repo reflow.Repository) (digest.Digest, error) {
	var copy Bundle
	copy = *p
	var mu sync.Mutex
	m := map[string]digest.Digest{}
	g, ctx := errgroup.WithContext(ctx)
	for k, v := range copy.Inline {
		k, v := k, v
		g.Go(func() error {
			d, err := repo.Put(ctx, bytes.NewReader(v))
			if err != nil {
				return err
			}
			mu.Lock()
			m[k] = d
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return digest.Digest{}, err
	}
	copy.Files = m
	buf, err := json.Marshal(copy)
	if err != nil {
		return digest.Digest{}, nil
	}
	d, err := repo.Put(ctx, bytes.NewReader(buf))
	if err != nil {
		return digest.Digest{}, err
	}
	return d, nil
}
