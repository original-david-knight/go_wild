package main

import (
	"strings"
	"sync"
)

type lineReader interface {
	Readline() (string, error)
}

// asyncInputReader ensures there is only ever one outstanding Readline call.
// Heartbeats and task-loop runs can otherwise race each other and split a
// single structured manager message across multiple readers.
type asyncInputReader struct {
	reader lineReader
	output func()
	ch     chan<- inputResult

	mu      sync.Mutex
	running bool
}

func newAsyncInputReader(reader lineReader, prompt func(), ch chan<- inputResult) *asyncInputReader {
	return &asyncInputReader{
		reader: reader,
		output: prompt,
		ch:     ch,
	}
}

func (r *asyncInputReader) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	if r.output != nil {
		r.output()
	}

	go func() {
		line, err := r.reader.Readline()

		r.mu.Lock()
		r.running = false
		r.mu.Unlock()

		r.ch <- inputResult{line: strings.TrimSpace(line), err: err}
	}()
}
