package main

import "github.com/spf13/pflag"

// flagValueWrapper delegates all functionality to inner pflag.Value
type flagValueWrapper struct {
	inner pflag.Value
}

var _ pflag.Value = new(flagValueWrapper)

func (l *flagValueWrapper) String() string {
	return l.inner.String()
}

func (l *flagValueWrapper) Set(value string) error {
	return l.inner.Set(value)
}

func (l *flagValueWrapper) Type() string {
	return l.inner.Type()
}
