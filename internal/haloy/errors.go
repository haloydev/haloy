package haloy

const (
	ExitSuccess = 0
	ExitError   = 1
)

type PrefixedError struct {
	Err    error
	Prefix string
}

func (e *PrefixedError) Error() string {
	return e.Err.Error()
}

func (e *PrefixedError) Unwrap() error {
	return e.Err
}

func (e *PrefixedError) GetPrefix() string {
	return e.Prefix
}

func getExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}
	return ExitError
}
