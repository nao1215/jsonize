// Package limits holds the bounds a reading shares with every other
// reading of the same input, so that a document is not refused by one
// and read by another.
//
// The bounds on bytes, on the length of a record and on the values a
// reading retains belong to the engine, which every reading of command
// output goes through; this is for the ones it has no say in.
package limits

// MaxDepth bounds how deeply arrays and objects, sequences and mappings
// may nest in a document jz reads. A document nested deeper is refused
// rather than read, so no input can exhaust the stack, and the same
// document is refused whether it is written as JSON or as YAML.
const MaxDepth = 1000
