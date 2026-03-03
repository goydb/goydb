//go:build !nototp

package handler

import (
	"errors"

	"github.com/goydb/goydb/pkg/model"
)

func init() {
	RegisterFeature("totp")
	RegisterTOTPCheck(func(user *model.User, token string) error {
		if !user.HasTOTP() {
			return nil
		}
		if token == "" {
			return errors.New("TOTP token required")
		}
		ok, err := user.TOTP.VerifyTOTP(token)
		if err != nil || !ok {
			return errors.New("Invalid TOTP token")
		}
		return nil
	})
}
