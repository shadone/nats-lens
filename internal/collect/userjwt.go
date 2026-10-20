package collect

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

// UserJWT is what the collector keeps of a connection's user JWT: never
// the token, only its expiry.
type UserJWT struct {
	// Decoded is false when the claims segment could not be read.
	Decoded bool
	// Expires is zero when the JWT does not expire or did not decode.
	Expires time.Time
}

// takeUserJWTs reads the expiry of every user JWT CONNZ reported, by
// connection ID, and removes the JWTs from the reply. The server verified
// the JWT when the client connected; nats-lens only displays its expiry,
// so the claims are read without verifying the signature. CONNZ reports a
// bearer JWT as empty, so such a connection has no entry.
func takeUserJWTs(c *server.Connz) map[uint64]UserJWT {
	if c == nil {
		return nil
	}
	var out map[uint64]UserJWT
	for _, ci := range c.Conns {
		if ci == nil || ci.JWT == "" {
			continue
		}
		if out == nil {
			out = map[uint64]UserJWT{}
		}
		out[ci.Cid] = readUserJWT(ci.JWT)
		ci.JWT = ""
	}
	return out
}

// readUserJWT reads the expiry from a JWT's claims segment.
func readUserJWT(token string) UserJWT {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return UserJWT{}
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return UserJWT{}
	}
	var claims struct {
		Expires int64 `json:"exp"`
	}
	if err := json.Unmarshal(b, &claims); err != nil {
		return UserJWT{}
	}
	return UserJWT{Decoded: true, Expires: unixTime(claims.Expires)}
}

// logUndecodedJWTs logs, at debug level, each connection whose user JWT
// did not decode, without the JWT.
func (c *Collector) logUndecodedJWTs(snap *Snapshot) {
	for _, v := range snap.SortedViews() {
		if v.Connz == nil {
			continue
		}
		for _, ci := range v.Connz.Conns {
			if ci == nil {
				continue
			}
			if j, ok := v.UserJWTs[ci.Cid]; ok && !j.Decoded {
				c.log.Debug("user JWT did not decode", "server", v.Info.Name, "cid", ci.Cid, "account", ci.Account)
			}
		}
	}
}
