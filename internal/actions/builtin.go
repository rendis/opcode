package actions

import "github.com/rendis/opcode/internal/validation"

// RegisterBuiltins registers all built-in actions in the given registry.
func RegisterBuiltins(reg *Registry, validator *validation.JSONSchemaValidator, httpCfg HTTPConfig, fsCfg FSConfig, shellCfg ShellConfig) error {
	all := make([]Action, 0, 32)

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

	// Filesystem actions.
	all = append(all, FSActions(fsCfg)...)

	// Shell actions.
	all = append(all, ShellActions(shellCfg)...)

	for _, a := range all {
		if err := reg.Register(a); err != nil {
			return err
		}
	}
	return nil
}
