// Package api implements `gentle-ai api <method>`: a headless, versioned JSON
// interface to everything the TUI main menu offers. Parameters arrive as one
// JSON object on stdin; stdout carries newline-delimited JSON events followed by
// exactly one result or error line. Methods call the same services and flow
// rules the TUI uses and never prompt, need a terminal, or self-update.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Schema identifies the envelope of every final line.
const Schema = "gentle-ai.api/v1"

// Version is the API contract version reported by describe.
const Version = 1

// ErrorCode classifies a failed call.
type ErrorCode string

const (
	CodeInvalidParams ErrorCode = "invalid_params"
	CodeNotFound      ErrorCode = "not_found"
	CodeUnsupported   ErrorCode = "unsupported"
	CodeConflict      ErrorCode = "conflict"
	CodeFailed        ErrorCode = "failed"
)

// Error is the error payload of the final line. Handlers return it to choose a
// code; any other error is reported as failed.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

func errorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func invalidParams(format string, args ...any) *Error {
	return errorf(CodeInvalidParams, format, args...)
}

func asError(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return &Error{Code: CodeFailed, Message: err.Error()}
}

type resultLine struct {
	Type   string `json:"type"`
	Schema string `json:"schema"`
	Data   any    `json:"data"`
}

type errorLine struct {
	Type   string `json:"type"`
	Schema string `json:"schema"`
	Error  *Error `json:"error"`
}

// methodFunc runs one method with its raw JSON params.
type methodFunc func(ctx context.Context, env *env, params []byte) (any, error)

type method struct {
	name string
	run  methodFunc
}

// env is what a handler receives: injected dependencies and the event stream.
type env struct {
	deps   Deps
	events *emitter
}

// typed decodes the params into P, rejecting unknown fields, before running fn.
func typed[P any](fn func(ctx context.Context, env *env, params P) (any, error)) methodFunc {
	return func(ctx context.Context, env *env, raw []byte) (any, error) {
		params, err := decodeParams[P](raw)
		if err != nil {
			return nil, err
		}
		return fn(ctx, env, params)
	}
}

type noParams struct{}

func decodeParams[P any](raw []byte) (P, error) {
	var params P
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	if trimmed[0] != '{' {
		return params, invalidParams("params must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return params, invalidParams("%s", strings.TrimPrefix(err.Error(), "json: "))
	}
	if decoder.More() {
		return params, invalidParams("params must be a single JSON object")
	}
	return params, nil
}

func lookup(name string) (method, bool) {
	for _, m := range methods() {
		if m.name == name {
			return m, true
		}
	}
	return method{}, false
}

// MethodNames lists every registered method, sorted.
func MethodNames() []string {
	names := make([]string, 0, len(methods()))
	for _, m := range methods() {
		names = append(names, m.name)
	}
	sort.Strings(names)
	return names
}

// Run executes `gentle-ai api <method>` for args (the words after "api") and
// writes its NDJSON stream to stdout. It returns a non-nil error exactly when
// the final line is an error, so the caller exits 1.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, deps Deps) error {
	events := newEmitter(stdout)
	var (
		data any
		err  error
	)
	switch len(args) {
	case 0:
		err = invalidParams("usage: gentle-ai api <method> (params as one JSON object on stdin); run `gentle-ai api describe` for the methods")
	case 1:
		data, err = call(ctx, args[0], stdin, events, deps)
	default:
		err = invalidParams("unexpected argument %q: pass params as one JSON object on stdin", args[1])
	}
	if err != nil {
		apiErr := asError(err)
		events.write(errorLine{Type: "error", Schema: Schema, Error: apiErr})
		return apiErr
	}
	events.write(resultLine{Type: "result", Schema: Schema, Data: data})
	return nil
}

// Unavailable reports a call that cannot start, such as one without a home
// directory, as the single error line of the stream.
func Unavailable(stdout io.Writer, err error) error {
	apiErr := asError(err)
	newEmitter(stdout).write(errorLine{Type: "error", Schema: Schema, Error: apiErr})
	return apiErr
}

func call(ctx context.Context, name string, stdin io.Reader, events *emitter, deps Deps) (any, error) {
	m, ok := lookup(name)
	if !ok {
		return nil, errorf(CodeUnsupported, "unknown method %q; run `gentle-ai api describe` for the supported methods", name)
	}
	var raw []byte
	if stdin != nil {
		var err error
		if raw, err = io.ReadAll(stdin); err != nil {
			return nil, invalidParams("read params: %v", err)
		}
	}
	return m.run(ctx, &env{deps: deps, events: events}, raw)
}
