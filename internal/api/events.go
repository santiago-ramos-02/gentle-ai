package api

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/gentleman-programming/gentle-ai/v3/internal/pipeline"
)

// emitter serializes NDJSON lines. Progress callbacks can arrive from worker
// goroutines, so every line is written whole under one lock.
type emitter struct {
	mu  sync.Mutex
	out io.Writer
}

func newEmitter(out io.Writer) *emitter { return &emitter{out: out} }

func (e *emitter) write(line any) {
	data, err := json.Marshal(line)
	if err != nil {
		data, _ = json.Marshal(errorLine{Type: "error", Schema: Schema, Error: &Error{Code: CodeFailed, Message: "encode output: " + err.Error()}})
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.out.Write(append(data, '\n'))
}

type progressLine struct {
	Type   string `json:"type"`
	Step   string `json:"step"`
	Label  string `json:"label"`
	Stage  string `json:"stage,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type logLine struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// progress reports one pipeline step transition.
func (e *emitter) progress(event pipeline.ProgressEvent) {
	line := progressLine{Type: "progress", Step: event.StepID, Label: stepLabel(event.StepID), Stage: string(event.Stage), Status: string(event.Status)}
	if event.Err != nil {
		line.Error = event.Err.Error()
	}
	e.write(line)
}

func (e *emitter) log(message string) {
	e.write(logLine{Type: "log", Message: message})
}

// logWriter turns free-form output (upgrade progress, tool commands) into one
// log event per completed line, flushed as soon as the line ends. A carriage
// return redraws the current line, as on a terminal, so spinner frames
// collapse into the final text of their line.
func (e *emitter) logWriter() *lineLogger { return &lineLogger{events: e} }

type lineLogger struct {
	mu      sync.Mutex
	events  *emitter
	pending bytes.Buffer
	// carriage marks a carriage return not yet resolved: a newline right
	// after it ends the line (CRLF); anything else redraws it.
	carriage bool
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, b := range p {
		if l.carriage && b != '\n' {
			l.pending.Reset()
		}
		l.carriage = false
		switch b {
		case '\n':
			l.emit(l.pending.String())
			l.pending.Reset()
		case '\r':
			l.carriage = true
		default:
			l.pending.WriteByte(b)
		}
	}
	return len(p), nil
}

// Flush emits a trailing partial line.
func (l *lineLogger) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending.Len() > 0 {
		l.emit(l.pending.String())
		l.pending.Reset()
	}
}

func (l *lineLogger) emit(line string) {
	if strings.TrimSpace(line) != "" {
		l.events.log(line)
	}
}
