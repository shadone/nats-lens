package api

import (
	"strings"
	"unicode/utf8"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// FindingAccount is the account an entity belongs to, as the servers
// report it: an account's own key, the first part of a stream's or a
// consumer's key. It is "" for an entity of no one account: a server, a
// client (a connection name, which several accounts can use), a
// connection and the system.
func FindingAccount(e *v1.EntityRef) string {
	switch e.GetType() {
	case v1.EntityType_ENTITY_TYPE_ACCOUNT:
		return e.GetKey()
	case v1.EntityType_ENTITY_TYPE_STREAM, v1.EntityType_ENTITY_TYPE_CONSUMER:
		account, _, _ := strings.Cut(e.GetKey(), "/")
		return account
	}
	return ""
}

// OtherAccount reports whether ListFindings' account filter leaves the
// finding out: an account is asked for and the finding's entity belongs to
// another one. A finding of no one account concerns every account and is
// kept.
func OtherAccount(f *v1.Finding, account string) bool {
	if account == "" {
		return false
	}
	own := FindingAccount(f.GetEntity())
	return own != "" && own != account
}

// checkAccountFilter checks a request's account filter.
func checkAccountFilter(account string) error {
	if utf8.RuneCountInString(account) > MaxNameLength {
		return invalid("account has more than %d characters", MaxNameLength)
	}
	return nil
}
