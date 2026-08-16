package main

// cliExit is a Cobra RunE error that sets a process exit code.
// Code 1 with a nil Err is silent (stdout already has the diff report).
type cliExit struct {
	Code int
	Err  error
}

func (e *cliExit) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *cliExit) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func exitErr(code int, err error) error {
	return &cliExit{Code: code, Err: err}
}

func exitSilent(code int) error {
	return &cliExit{Code: code}
}
