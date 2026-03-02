package handler

import "github.com/goydb/goydb/pkg/model"

// hashUserPassword converts a plaintext "password" field in a _users document
// to the CouchDB-compatible PBKDF2 derived key fields. It is a no-op for
// non-_users databases, documents without a password field, or deleted docs.
func hashUserPassword(dbName string, doc *model.Document) error {
	if dbName != "_users" {
		return nil
	}
	if doc.Deleted {
		return nil
	}

	pw, _ := doc.Data["password"].(string)
	if pw == "" {
		return nil
	}

	var u model.User
	if err := u.FromDocument(doc); err != nil {
		return err
	}

	if err := u.GeneratePBKDF2(); err != nil {
		return err
	}

	doc.Data["derived_key"] = u.DerivedKey
	doc.Data["salt"] = u.Salt
	doc.Data["iterations"] = u.Iterations
	doc.Data["password_scheme"] = "pbkdf2"
	delete(doc.Data, "password")

	return nil
}
