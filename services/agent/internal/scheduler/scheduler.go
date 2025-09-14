package scheduler

import (
	"sync"
)

// Scheduler manages concurrent job execution with a worker pool
type Scheduler struct {
	workers    int
	jobQueue   chan func()
	wg         sync.WaitGroup
	started    bool
	mu         sync.Mutex
}

// New creates a new scheduler with the specified number of workers
func New(workers int) *Scheduler {
	return &Scheduler{
		workers:  workers,
		jobQueue: make(chan func(), workers*2), // Buffer for better performance
	}
}

// Submit adds a job to the scheduler
func (s *Scheduler) Submit(job func()) {
	s.mu.Lock()
	if !s.started {
		s.start()
	}
	s.mu.Unlock()

	s.wg.Add(1)
	s.jobQueue <- func() {
		defer s.wg.Done()
		job()
	}
}

// start initializes the worker goroutines
func (s *Scheduler) start() {
	if s.started {
		return
	}
	s.started = true

	for i := 0; i < s.workers; i++ {
		go s.worker()
	}
}

// worker processes jobs from the queue
func (s *Scheduler) worker() {
	for job := range s.jobQueue {
		job()
	}
}

// Wait blocks until all submitted jobs are completed
func (s *Scheduler) Wait() {
	s.wg.Wait()
}

// Close shuts down the scheduler
func (s *Scheduler) Close() {
	close(s.jobQueue)
}
