package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Notifications configures the channels findings are sent to. It is shared
// by every system, as checks is; a systems entry cannot set it.
type Notifications struct {
	// UIURL is the address the UI is reached at, for the links in
	// messages; empty means messages carry no link.
	UIURL string `yaml:"ui_url"`
	// Channels are the receivers.
	Channels []Channel `yaml:"channels"`
}

// Channel types.
const (
	ChannelWebhook = "webhook"
	ChannelSlack   = "slack"
)

// ChannelKinds are the message kinds a channel can select with kinds, in
// the order the documentation lists them.
var ChannelKinds = []string{"firing", "changed", "resolved", "flapping", "muted", "reminder", "acknowledged"}

// Severities are the finding severities, lowest first.
var Severities = []string{"info", "warning", "critical"}

// Channel limits and defaults.
const (
	DefaultChannelTimeout     = 10 * time.Second
	MaxChannelTimeout         = 60 * time.Second
	DefaultChannelGiveUpAfter = 24 * time.Hour
	MinChannelRepeatEvery     = time.Hour
	DefaultChannelMinSeverity = "warning"
)

// channelName is the form of a channel name: it is a metric label, like a
// system id.
var channelName = systemID

// Channel is one receiver of notifications. Exactly one of URL, URLFile
// and URLEnv is set; a URL that holds a secret belongs in a file or a
// variable, which are read at each delivery attempt.
type Channel struct {
	// Name identifies the channel in logs and metric labels.
	Name string `yaml:"name"`
	// Type is webhook (the documented JSON body) or slack (a Slack-
	// compatible incoming webhook, a text message).
	Type string `yaml:"type"`
	// URL is the receiver's address, for a URL that holds no secret.
	URL string `yaml:"url"`
	// URLFile is a file holding the URL.
	URLFile string `yaml:"url_file"`
	// URLEnv is an environment variable holding the URL.
	URLEnv string `yaml:"url_env"`
	// BearerTokenFile is a file holding a token sent as
	// "Authorization: Bearer"; webhook only.
	BearerTokenFile string `yaml:"bearer_token_file"`
	// BearerTokenEnv is an environment variable holding the token.
	BearerTokenEnv string `yaml:"bearer_token_env"`
	// MinSeverity is the lowest severity the channel receives.
	MinSeverity string `yaml:"min_severity"`
	// Systems are the system ids the channel receives; empty means every
	// system.
	Systems []string `yaml:"systems"`
	// Checks are the check codes the channel receives; empty means every
	// check.
	Checks []string `yaml:"checks"`
	// ExcludeChecks are check codes the channel never receives.
	ExcludeChecks []string `yaml:"exclude_checks"`
	// Categories are the check categories the channel receives; empty
	// means every category.
	Categories []string `yaml:"categories"`
	// Kinds are the message kinds the channel receives; empty means every
	// kind.
	Kinds []string `yaml:"kinds"`
	// RepeatEvery sends a finding still firing again after this long; 0
	// sends no reminders.
	RepeatEvery Duration `yaml:"repeat_every"`
	// Timeout bounds one request.
	Timeout Duration `yaml:"timeout"`
	// GiveUpAfter is how long a message is retried before it is given up.
	GiveUpAfter Duration `yaml:"give_up_after"`
}

// UnmarshalYAML starts each channel from its defaults, so that a key left
// out keeps its default. It uses the decoder's own unmarshal function, so
// unknown keys are still refused.
func (c *Channel) UnmarshalYAML(unmarshal func(any) error) error {
	type plain Channel
	p := plain(DefaultChannel())
	if err := unmarshal(&p); err != nil {
		return channelKeyError(err, p.Name)
	}
	*c = Channel(p)
	return nil
}

// unknownField is the decoder's text for an unknown key of a channel,
// which names the local type plain.
var unknownField = regexp.MustCompile(`field (\S+) not found in type config\.plain`)

// channelKeyError rewords the decoder's unknown keys as "unknown key K in
// notifications channel N".
func channelKeyError(err error, name string) error {
	var te *yaml.TypeError
	if !errors.As(err, &te) {
		return err
	}
	where := "notifications channel " + name
	if name == "" {
		where = "a notifications channel without a name"
	}
	out := &yaml.TypeError{Errors: make([]string, len(te.Errors))}
	for i, e := range te.Errors {
		out.Errors[i] = unknownField.ReplaceAllString(e, "unknown key $1 in "+where)
	}
	return out
}

// DefaultChannel is a channel with every default and no name, type or
// destination.
func DefaultChannel() Channel {
	return Channel{
		MinSeverity: DefaultChannelMinSeverity,
		Timeout:     Duration(DefaultChannelTimeout),
		GiveUpAfter: Duration(DefaultChannelGiveUpAfter),
	}
}

// Sends reports whether the channel receives messages of the kind. Kinds
// outside ChannelKinds (a test message) are always sent.
func (c Channel) Sends(kind string) bool {
	if len(c.Kinds) == 0 || !slices.Contains(ChannelKinds, kind) {
		return true
	}
	return slices.Contains(c.Kinds, kind)
}

// SeverityRank orders severities: info 0, warning 1, critical 2; an
// unknown severity is -1.
func SeverityRank(s string) int { return slices.Index(Severities, s) }

// Accepts reports whether a finding of the system, check, category and
// severity is in the channel's view, by its filters alone.
func (c Channel) Accepts(system, check, category, severity string) bool {
	switch {
	case SeverityRank(severity) < SeverityRank(c.MinSeverity):
		return false
	case len(c.Systems) > 0 && !slices.Contains(c.Systems, system):
		return false
	case len(c.Checks) > 0 && !slices.Contains(c.Checks, check):
		return false
	case slices.Contains(c.ExcludeChecks, check):
		return false
	case len(c.Categories) > 0 && !slices.Contains(c.Categories, category):
		return false
	}
	return true
}

// CheckChannelURL refuses a receiver URL that is not https, or http on a
// loopback host. The error never holds the URL, which can be a secret.
func CheckChannelURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("the URL does not parse as an absolute URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if loopbackHost(u.Hostname()) {
			return nil
		}
		return errors.New("an http URL is accepted only for a loopback host; use https")
	}
	return errors.New("the URL must be https")
}

func loopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// RedactChannelURL is the URL reduced to its scheme and host, as logs and
// messages name a receiver; "[invalid]" when it does not parse.
func RedactChannelURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "[invalid]"
	}
	return u.Scheme + "://" + u.Host
}

// validate checks the block's shape. Check codes and categories are
// checked against the loaded checks when natslens starts, not here.
// systemIDs are the configured systems.
func (n Notifications) validate(systemIDs []string) []error {
	var errs []error
	if n.UIURL != "" {
		if u, err := url.Parse(n.UIURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, errors.New("notifications.ui_url must be an absolute http or https URL"))
		}
	}
	seen := map[string]bool{}
	for i, ch := range n.Channels {
		p := fmt.Sprintf("notifications.channels[%d]", i)
		if ch.Name != "" {
			p += " (" + ch.Name + ")"
		}
		switch {
		case !channelName.MatchString(ch.Name):
			errs = append(errs, fmt.Errorf("%s: name %q must match %s", p, ch.Name, channelName))
		case seen[ch.Name]:
			errs = append(errs, fmt.Errorf("%s: duplicate name", p))
		}
		seen[ch.Name] = true
		errs = append(errs, prefixErrors(p, ch.validate(systemIDs))...)
	}
	return errs
}

func (c Channel) validate(systemIDs []string) []error {
	var errs []error
	if c.Type != ChannelWebhook && c.Type != ChannelSlack {
		errs = append(errs, fmt.Errorf("type %q must be webhook or slack", c.Type))
	}
	set := 0
	for _, v := range []string{c.URL, c.URLFile, c.URLEnv} {
		if v != "" {
			set++
		}
	}
	if set != 1 {
		errs = append(errs, errors.New("set exactly one of url, url_file and url_env"))
	}
	if c.URL != "" {
		if err := CheckChannelURL(c.URL); err != nil {
			errs = append(errs, fmt.Errorf("url: %w", err))
		}
	}
	if c.BearerTokenFile != "" && c.BearerTokenEnv != "" {
		errs = append(errs, errors.New("set at most one of bearer_token_file and bearer_token_env"))
	}
	if c.Type == ChannelSlack && (c.BearerTokenFile != "" || c.BearerTokenEnv != "") {
		errs = append(errs, errors.New("a bearer token is for a webhook channel only; a slack channel's URL is its secret"))
	}
	if SeverityRank(c.MinSeverity) < 0 {
		errs = append(errs, fmt.Errorf("min_severity %q must be info, warning or critical", c.MinSeverity))
	}
	for _, s := range c.Systems {
		if !slices.Contains(systemIDs, s) {
			errs = append(errs, fmt.Errorf("systems: unknown system %q; configured systems: %s", s, strings.Join(systemIDs, ", ")))
		}
	}
	for _, k := range c.Kinds {
		if !slices.Contains(ChannelKinds, k) {
			errs = append(errs, fmt.Errorf("kinds: unknown kind %q; kinds are %s", k, strings.Join(ChannelKinds, ", ")))
		}
	}
	if r := c.RepeatEvery.D(); r < 0 || (r > 0 && r < MinChannelRepeatEvery) {
		errs = append(errs, errors.New("repeat_every must be 0 (no reminders) or at least 1h"))
	}
	if t := c.Timeout.D(); t <= 0 || t > MaxChannelTimeout {
		errs = append(errs, errors.New("timeout must be positive and at most 60s"))
	}
	if c.GiveUpAfter.D() <= 0 {
		errs = append(errs, errors.New("give_up_after must be positive"))
	}
	return errs
}

// Channel returns the channel with the name.
func (n Notifications) Channel(name string) (Channel, bool) {
	for _, c := range n.Channels {
		if c.Name == name {
			return c, true
		}
	}
	return Channel{}, false
}

// ChannelNames lists the channels' names in the file's order.
func (n Notifications) ChannelNames() []string {
	out := make([]string, len(n.Channels))
	for i, c := range n.Channels {
		out[i] = c.Name
	}
	return out
}
