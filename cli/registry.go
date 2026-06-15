package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// registry maps environment names to their direct clients. In --direct mode there
// is a single environment ("local"); the structure anticipates the multi-origin
// relay of Phase 2.
type registry struct {
	mu   sync.RWMutex
	envs map[string]*directClient
}

func newRegistry() *registry {
	return &registry{envs: make(map[string]*directClient)}
}

func (r *registry) add(name string, c *directClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.envs[name] = c
}

// resolve selects the client for the named environment. An empty name resolves to
// the sole environment when exactly one is connected; otherwise it is an error
// listing the available environments.
func (r *registry) resolve(name string) (*directClient, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		if len(r.envs) == 1 {
			for _, c := range r.envs {
				return c, nil
			}
		}
		return nil, fmt.Errorf("environment is required; connected: %s", strings.Join(r.namesLocked(), ", "))
	}

	c, ok := r.envs[name]
	if !ok {
		return nil, fmt.Errorf("unknown environment %q; connected: %s", name, strings.Join(r.namesLocked(), ", "))
	}
	return c, nil
}

type environmentInfo struct {
	Name    string `json:"name"`
	Session string `json:"session"`
}

func (r *registry) list() []environmentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]environmentInfo, 0, len(r.envs))
	for name, c := range r.envs {
		out = append(out, environmentInfo{Name: name, Session: c.sid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *registry) namesLocked() []string {
	names := make([]string, 0, len(r.envs))
	for name := range r.envs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
