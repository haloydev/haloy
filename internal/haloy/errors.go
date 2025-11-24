package haloy

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
