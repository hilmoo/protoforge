//go:build windows

package runner

import "os"

func interruptSignals() []os.Signal { return []os.Signal{os.Interrupt} }
