// sbx/internal/driver/fake.go
package driver

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Fake struct {
	mu       sync.Mutex
	seq      int
	envs     map[string]envRec
	LastSpec EnvSpec
}

type envRec struct {
	env     Env
	session string
}

func NewFake() *Fake { return &Fake{envs: map[string]envRec{}} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Create(_ context.Context, sessionID string, spec EnvSpec) (Env, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.LastSpec = spec
	var id, name string
	if spec.Name != "" {
		id = spec.Name
		name = spec.Name
	} else {
		f.seq++
		id = fmt.Sprintf("env%03d", f.seq)
		short := sessionID
		if len(short) > 5 {
			short = short[:5]
		}
		name = "sbx-" + short + "-" + id
	}
	e := Env{
		ID:        id,
		Name:      name,
		Namespace: name,
		Status:    "running",
		Ports:     nil,
	}
	f.envs[id] = envRec{env: e, session: sessionID}
	return e, nil
}

func (f *Fake) Exec(_ context.Context, id string, cmd []string) (ExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.envs[id]; !ok {
		return ExecResult{}, fmt.Errorf("environment %q not found", id)
	}
	return ExecResult{ExitCode: 0, Stdout: strings.Join(cmd, " ") + "\n"}, nil
}

func (f *Fake) Logs(_ context.Context, id string, _ LogOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.envs[id]; !ok {
		return "", fmt.Errorf("environment %q not found", id)
	}
	return "fake logs for " + id + "\n", nil
}

func (f *Fake) Status(_ context.Context, id string) (Env, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.envs[id]
	if !ok {
		return Env{}, fmt.Errorf("environment %q not found", id)
	}
	return r.env, nil
}

func (f *Fake) List(_ context.Context, sessionID string) ([]Env, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Env
	for _, r := range f.envs {
		if r.session == sessionID {
			out = append(out, r.env)
		}
	}
	return out, nil
}

func (f *Fake) Destroy(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.envs[id]; !ok {
		return fmt.Errorf("environment %q not found", id)
	}
	delete(f.envs, id)
	return nil
}
