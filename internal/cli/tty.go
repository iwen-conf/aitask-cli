package cli

import (
	"io"
	"os"
)

// isInteractiveStdin reports whether stdin is connected to a terminal. The TUI
// only launches when both stdin and stdout are TTYs to avoid hijacking pipes
// and CI/scripted invocations.
func isInteractiveStdin(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func isInteractiveWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
