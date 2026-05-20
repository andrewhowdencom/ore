package app

import (
	"fmt"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/spf13/viper"
)

// ConduitFactory creates a conduit from runtime options and a session manager.
type ConduitFactory func(mgr *session.Manager, opts map[string]any) (conduit.Conduit, error)

// HandlerFactory creates a handler from runtime options.
type HandlerFactory func(opts map[string]any) (loop.Handler, error)

// ConduitRegistration holds compile-time defaults for a conduit.
type ConduitRegistration struct {
	Name     string
	Factory  ConduitFactory
	Defaults map[string]any
}

// HandlerRegistration holds compile-time defaults for a handler.
type HandlerRegistration struct {
	Name     string
	Factory  HandlerFactory
	Defaults map[string]any
}

// TransformFactory creates a transform from runtime options.
type TransformFactory func(opts map[string]any) (loop.Transform, error)

// TransformRegistration holds compile-time defaults for a transform.
type TransformRegistration struct {
	Name     string
	Factory  TransformFactory
	Defaults map[string]any
}

// getOpts reads a nested map[string]any from Viper using dot notation.
// For example, getOpts(v, "conduits", "http") returns the value at
// key "conduits.http" if it is a map; otherwise it returns nil.
func getOpts(v *viper.Viper, prefix, name string) map[string]any {
	key := fmt.Sprintf("%s.%s", prefix, name)
	val := v.Get(key)
	if val == nil {
		return nil
	}
	m, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	return m
}
