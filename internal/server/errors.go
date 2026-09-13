package server

import "github.com/mmedum/favro-mcp/internal/render"

// classedError is an error that names its own class, so the boundary
// can render "[class] message" without matching on the text. Every
// package-level sentinel in this package is one — asserted by
// TestEverySentinelIsClassified, which reads the source rather than
// trusting this comment.
//
// Wrapping with fmt.Errorf("%w: …", sentinel) keeps the class, because
// render.Classify walks the chain with errors.As.
type classedError struct {
	class render.Class
	msg   string
}

func (e *classedError) Error() string { return e.msg }

// ErrorClass satisfies render.Classed.
func (e *classedError) ErrorClass() render.Class { return e.class }

// classed builds a sentinel. The class is the first argument because
// it is the part a reader is checking when they look at one of these.
func classed(class render.Class, msg string) error {
	return &classedError{class: class, msg: msg}
}
