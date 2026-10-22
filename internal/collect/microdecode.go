package collect

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

// userInfoSubject answers which account a connection's user is in.
const userInfoSubject = "$SYS.REQ.USER.INFO"

// microAsked returns the state of a request from its exchange, the number
// of replies that decoded and failed to, and the first decode error.
func microAsked(r MicroRequest, x *exchanged, decoded, failed int, firstErr error) MicroAsked {
	a := MicroAsked{Request: r, Replies: decoded}
	switch {
	case decoded > 0:
		a.State = MicroOK
	case failed > 0:
		a.State = MicroError
	case x == nil:
		a.State, a.Error = MicroError, "not sent"
		return a
	case x.err != nil:
		a.State, a.Error = MicroError, x.err.Error()
		return a
	case x.noResponders:
		a.State = MicroNoResponders
		return a
	default:
		a.State = MicroTimeout
		return a
	}
	if failed > 0 {
		a.Error = fmt.Sprintf("%d of %d replies did not decode: %v", failed, decoded+failed, firstErr)
	}
	return a
}

// decodeStats decodes the STATS replies, one per instance: a second reply
// of the same instance is dropped.
func decodeStats(x *exchanged) (MicroAsked, []micro.Stats) {
	var out []micro.Stats
	failed := 0
	var firstErr error
	seen := map[instanceKey]bool{}
	for _, b := range replies(x) {
		var s micro.Stats
		err := json.Unmarshal(b, &s)
		if err == nil {
			err = checkIdentity(s.Type, micro.StatsResponseType, s.ServiceIdentity)
		}
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		k := instanceKey{s.Name, s.ID}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return lessIdentity(out[i].ServiceIdentity, out[j].ServiceIdentity) })
	return microAsked(MicroStats, x, len(out), failed, firstErr), out
}

// decodeInfos decodes the INFO replies as decodeStats does the STATS.
func decodeInfos(x *exchanged) (MicroAsked, []micro.Info) {
	var out []micro.Info
	failed := 0
	var firstErr error
	seen := map[instanceKey]bool{}
	for _, b := range replies(x) {
		var in micro.Info
		err := json.Unmarshal(b, &in)
		if err == nil {
			err = checkIdentity(in.Type, micro.InfoResponseType, in.ServiceIdentity)
		}
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		k := instanceKey{in.Name, in.ID}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool { return lessIdentity(out[i].ServiceIdentity, out[j].ServiceIdentity) })
	return microAsked(MicroInfo, x, len(out), failed, firstErr), out
}

func replies(x *exchanged) [][]byte {
	if x == nil {
		return nil
	}
	return x.replies
}

// checkIdentity rejects a reply of another type or without a service name
// and instance ID.
func checkIdentity(got, want string, id micro.ServiceIdentity) error {
	if got != want {
		return fmt.Errorf("type %q, want %q", got, want)
	}
	if id.Name == "" || id.ID == "" {
		return errors.New("no service name or instance id")
	}
	return nil
}

func lessIdentity(a, b micro.ServiceIdentity) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// resolveMicroAccount records the account name the server gave for nc's
// user. A refused lookup is not made again for the label; a lookup that
// got no answer is made again next epoch.
func (c *Collector) resolveMicroAccount(label string, st *microAccount, nc *nats.Conn, x *exchanged) {
	if x == nil {
		return
	}
	if x.err != nil && errors.Is(x.err, nats.ErrPermissionViolation) {
		st.lookupDenied = true
		c.log.Info("account name lookup not permitted; micro services are stored under the configured label",
			"label", label, "subject", userInfoSubject)
		return
	}
	for _, b := range x.replies {
		var r struct {
			Data struct {
				Account string `json:"account"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &r) == nil && r.Data.Account != "" {
			st.resolvedOn, st.account = nc, r.Data.Account
			return
		}
	}
}
