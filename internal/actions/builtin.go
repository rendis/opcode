package actions

import "github.com/rendis/opcode/internal/validation"

// RegisterBuiltins registers all built-in actions in the given registry.
func RegisterBuiltins(reg *Registry, validator *validation.JSONSchemaValidator, httpCfg HTTPConfig) error {
	all := make([]Action, 0, 16)

	// HTTP actions.
	all = append(all,
		NewHTTPRequestAction(httpCfg),
		NewHTTPGetAction(httpCfg),
		NewHTTPPostAction(httpCfg),
	)

	// Crypto actions.
	all = append(all, CryptoActions()...)

	// Assert actions.
	all = append(all, AssertActions(validator)...)

	for _, a := range all {
		if err := reg.Register(a); err != nil {
			return err
		}
	}
	return nil
}
