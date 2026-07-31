package service

import "github.com/cpave3/legato/internal/engine/store"

// CheckoutInspector reads repository identity from an explicit local checkout.
type CheckoutInspector interface {
	InspectCodeContext(directory string) (*store.CodeContext, error)
}

// CheckoutInspectFunc adapts a composition-root function to CheckoutInspector.
type CheckoutInspectFunc func(directory string) (*store.CodeContext, error)

func (f CheckoutInspectFunc) InspectCodeContext(directory string) (*store.CodeContext, error) {
	return f(directory)
}
