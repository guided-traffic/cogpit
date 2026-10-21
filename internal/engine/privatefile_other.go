//go:build !windows

package engine

// heldOpenElsewhere is false: macOS and Linux open, rename and remove a file whoever has it open.
func heldOpenElsewhere(error) bool { return false }
