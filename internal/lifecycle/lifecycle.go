// Package lifecycle provides graceful shutdown management for background tasks.
package lifecycle

import (
	"context"
	"sync"
	"time"
)

// Task is a long-running component that can be started and stopped.
type Task interface {
	Run(ctx context.Context) error
	Name() string
}

// Manager coordinates startup and graceful shutdown of background tasks.
type Manager struct {
	tasks []Task
	done  chan struct{}
}

func NewManager() *Manager {
	return &Manager{done: make(chan struct{})}
}

func (m *Manager) Register(t Task) {
	m.tasks = append(m.tasks, t)
}

// Start launches all tasks concurrently.
func (m *Manager) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for _, t := range m.tasks {
		wg.Add(1)
		go func(task Task) {
			defer wg.Done()
			_ = task.Run(ctx)
		}(t)
	}
	go func() {
		wg.Wait()
		close(m.done)
	}()
}

// Shutdown waits for all tasks to complete or times out.
func (m *Manager) Shutdown(timeout time.Duration) error {
	select {
	case <-m.done:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}
