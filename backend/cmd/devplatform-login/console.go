package main

import "os"

// openConsole opens the real console device directly, bypassing
// whatever stdin/stdout have been redirected to. Git owns stdin/stdout
// during `get`/`store`/`erase` for its own credential protocol, so an
// interactive prompt can't use them — "CONIN$"/"CONOUT$" are Windows'
// special device names for the calling process's actual console
// regardless of redirection, the standard mechanism interactive
// command-line credential helpers use for exactly this situation.
func openConsole() (in, out *os.File, err error) {
	in, err = os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	out, err = os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, out, nil
}
