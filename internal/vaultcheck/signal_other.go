//go:build !unix

package vaultcheck

// signalExit has no meaning here: a Windows child has no death by signal.
func signalExit(error) (int, bool) { return 0, false }
