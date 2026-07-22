package slash

import (
	"github.com/andrewhowdencom/ore/session"
)

// NewCommandForTest builds a slash.Command with a populated session field.
// It exists solely to let external test packages (e.g. x/tool/set_model)
// exercise handlers that depend on Command.Session() without forcing the
// test to plumb a real session.Session through the registry's Intercept
// path.
//
// This function is not part of the public API. External test code that
// uses it should accept that the signature may change in lockstep with
// the Command struct itself. The "ForTest" suffix is the convention to
// flag this.
//
// Production code must never call this — the registry's Intercept is
// the only legitimate constructor of a non-nil Command.session.
func NewCommandForTest(name, input string, sess *session.Session) Command {
	return Command{
		Name:    name,
		Input:   input,
		session: sess,
	}
}
