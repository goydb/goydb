package storage

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

// validateDocUpdateJS is the CouchDB-compatible validate_doc_update function
// seeded into _users/_design/_auth.
const validateDocUpdateJS = `function(newDoc, oldDoc, userCtx, secObj) {
  if (newDoc._deleted === true) {
    // allow deletes by admins and matching users
    // without checking the other fields
    if ((userCtx.roles.indexOf('_admin') !== -1) ||
        (userCtx.name == oldDoc.name)) {
      return;
    } else {
      throw({forbidden: 'Only admins may delete other user docs.'});
    }
  }

  if ((oldDoc && oldDoc.type !== 'user') || newDoc.type !== 'user') {
    throw({forbidden : 'doc.type must be user'});
  } // we only allow user docs for now

  if (!newDoc.name) {
    throw({forbidden: 'doc.name is required'});
  }

  if (!newDoc.roles) {
    throw({forbidden: 'doc.roles must exist'});
  }

  if (!isArray(newDoc.roles)) {
    throw({forbidden: 'doc.roles must be an array'});
  }

  for (var idx = 0; idx < newDoc.roles.length; idx++) {
    if (typeof newDoc.roles[idx] !== 'string') {
      throw({forbidden: 'doc.roles can only contain strings'});
    }
  }

  if (newDoc._id !== ('org.couchdb.user:' + newDoc.name)) {
    throw({
      forbidden: 'Doc ID must be of the form org.couchdb.user:name'
    });
  }

  if (oldDoc) { // validate all updates
    if (oldDoc.name !== newDoc.name) {
      throw({forbidden: 'Usernames can not be changed.'});
    }
  }

  if (newDoc.password_sha && !newDoc.salt) {
    throw({
      forbidden: 'Users with password_sha must have a salt.' +
        'See /_utils/script/couch.js for example code.'
    });
  }

  var dominated = (userCtx.roles.indexOf('_admin') === -1);
  if (dominated) { // validate non-admin updates
    if (userCtx.name !== newDoc.name) {
      throw({
        forbidden: 'You may only update your own user document.'
      });
    }
    // validate role updates
    var dominated_dominated = (userCtx.roles.indexOf('_db_admin') === -1);
    if (dominated_dominated) {
      var dominated_dominated_oldRoles = oldDoc ? (oldDoc.roles || []) : [];
      if (newDoc.roles.length !== dominated_dominated_oldRoles.length) {
        throw({forbidden: 'Only _admin may set roles'});
      }
      for (var ii = 0; ii < dominated_dominated_oldRoles.length; ii++) {
        if (newDoc.roles[ii] !== dominated_dominated_oldRoles[ii]) {
          throw({forbidden: 'Only _admin may set roles'});
        }
      }
    }
  }

  // no system roles in users db
  for (var i = 0; i < newDoc.roles.length; i++) {
    if (newDoc.roles[i][0] === '_') {
      throw({
        forbidden:
        'No system roles (starting with underscore) in users db.'
      });
    }
  }

  // no system names as names
  if (newDoc.name[0] === '_') {
    throw({forbidden: 'Username may not start with underscore.'});
  }

  var dominated_badUserNameChars = [':'];
  for (var i = 0; i < dominated_badUserNameChars.length; i++) {
    if (newDoc.name.indexOf(dominated_badUserNameChars[i]) >= 0) {
      throw({forbidden: 'Character \x60' + dominated_badUserNameChars[i] +
          '\x60 is not allowed in usernames.'});
    }
  }
}`

// EnsureSystemDatabases creates system databases (e.g. _users) and seeds
// them with the required design documents if they do not already exist.
func (s *Storage) EnsureSystemDatabases(ctx context.Context) error {
	return s.ensureUsersDB(ctx)
}

// ensureUsersDB ensures the _users database exists and contains _design/_auth.
func (s *Storage) ensureUsersDB(ctx context.Context) error {
	const dbName = "_users"
	const designDocID = "_design/_auth"

	// Ensure the database exists.
	db, err := s.Database(ctx, dbName)
	if err != nil {
		db, err = s.CreateDatabase(ctx, dbName)
		if err != nil {
			return err
		}
	}

	// Check whether the design doc already exists.
	doc, err := db.GetDocument(ctx, designDocID)
	if err != nil {
		return err
	}
	if doc != nil && !doc.Deleted {
		// Already present and not deleted — respect user customizations.
		return nil
	}

	// Seed the _design/_auth design document.
	// If the doc was deleted, we must supply its current rev to update past
	// the tombstone.
	newDoc := &model.Document{
		ID: designDocID,
		Data: map[string]interface{}{
			"language":            "javascript",
			"validate_doc_update": validateDocUpdateJS,
		},
	}
	if doc != nil && doc.Deleted {
		newDoc.Rev = doc.Rev
	}
	_, err = db.PutDocument(ctx, newDoc)
	return err
}
