package handler

import "github.com/goydb/goydb/pkg/model"

// TOTPCheckFunc verifies a TOTP token for a user. Returns nil on success
// or an error with a user-facing message on failure.
type TOTPCheckFunc func(user *model.User, token string) error

var totpCheckFunc TOTPCheckFunc

// RegisterTOTPCheck sets the global TOTP verification function.
func RegisterTOTPCheck(fn TOTPCheckFunc) {
	totpCheckFunc = fn
}

// checkTOTP calls the registered TOTP check function, or no-ops if TOTP
// support is not compiled in.
func checkTOTP(user *model.User, token string) error {
	if totpCheckFunc == nil {
		return nil
	}
	return totpCheckFunc(user, token)
}
