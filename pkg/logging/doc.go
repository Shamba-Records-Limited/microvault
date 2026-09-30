// Package logging builds the process logger and carries per-unit-of-work
// attributes through a context.Context.
//
// Setup installs a JSON logger on stdout as the slog default, which also routes
// the standard library log package through it. With attaches attributes to a
// context; Handler appends them to every record logged through a *Context
// method with that context, so a request ID or loan ID set once at the entry
// point reaches every line below it.
package logging
