package translation

import (
	"context"
)

type Translator interface {
	// Choice gets a translation according to an integer value.
	Choice(key string, number int, options ...Option) (string, error)
	// Get the translation for the given key.
	Get(key string, options ...Option) (string, error)
	// GetFallback get the current application/context fallback locale.
	GetFallback() string
	// GetLocale get the current application/context locale.
	GetLocale() string
	// Has checks if a translation exists for a given key.
	Has(key string, options ...Option) bool
	// SetFallback set the current application/context fallback locale.
	SetFallback(locale string) context.Context
	// SetLocale set the current application/context locale.
	SetLocale(locale string) context.Context
}

type Option struct {
	Fallback *bool
	Locale   string
	Replace  map[string]string
}

func Bool(value bool) *bool {
	return &value
}
