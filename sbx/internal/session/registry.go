package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type EnvRecord struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Network   string `json:"network,omitempty"`
	Project   string `json:"project,omitempty"`
}

type Registry struct {
	SessionID     string      `json:"session_id"`
	Seq           int         `json:"seq"`
	Envs          []EnvRecord `json:"envs"`
	SupervisorPID int         `json:"supervisor_pid,omitempty"`
	DeadlineUnix  int64       `json:"deadline_unix,omitempty"`
	ProxyAddr     string      `json:"proxy_addr,omitempty"`
	Alive         bool        `json:"alive"`
	path          string      `json:"-"`
}

func registryPath(sessionID string) string {
	return filepath.Join(StateDir(sessionID), "registry.json")
}

func OpenRegistry(sessionID string) (*Registry, error) {
	path := registryPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("registry: mkdir: %w", err)
	}
	r := &Registry{SessionID: sessionID, Alive: true, Envs: []EnvRecord{}, path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := r.Save(); err != nil {
				return nil, err
			}
			return r, nil
		}
		return nil, fmt.Errorf("registry: read: %w", err)
	}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, fmt.Errorf("registry: corrupt at %s: %w", path, err)
	}
	r.path = path
	if r.Envs == nil {
		r.Envs = []EnvRecord{}
	}
	return r, nil
}

func (r *Registry) Path() string { return r.path }

func (r *Registry) Save() error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: encode: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("registry: write: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("registry: rename: %w", err)
	}
	return nil
}

func (r *Registry) NextSeq() (int, error) {
	r.Seq++
	if err := r.Save(); err != nil {
		r.Seq-- // best-effort rollback in memory
		return 0, err
	}
	return r.Seq, nil
}

func (r *Registry) Add(rec EnvRecord) error {
	for i, e := range r.Envs {
		if e.ID == rec.ID {
			r.Envs[i] = rec
			return r.Save()
		}
	}
	r.Envs = append(r.Envs, rec)
	return r.Save()
}

func (r *Registry) Remove(id string) error {
	out := r.Envs[:0]
	for _, e := range r.Envs {
		if e.ID != id {
			out = append(out, e)
		}
	}
	r.Envs = out
	return r.Save()
}

func (r *Registry) List() []EnvRecord {
	cp := make([]EnvRecord, len(r.Envs))
	copy(cp, r.Envs)
	return cp
}

func (r *Registry) SetSupervisor(pid int, deadline time.Time) error {
	r.SupervisorPID = pid
	r.Alive = true
	if deadline.IsZero() {
		r.DeadlineUnix = 0
	} else {
		r.DeadlineUnix = deadline.UTC().Unix()
	}
	return r.Save()
}

func (r *Registry) SetProxy(addr string) error {
	r.ProxyAddr = addr
	return r.Save()
}

func (r *Registry) MarkEnded() error {
	r.Alive = false
	r.SupervisorPID = 0
	r.DeadlineUnix = 0
	r.ProxyAddr = ""
	r.Envs = []EnvRecord{}
	return r.Save()
}
