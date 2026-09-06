package config

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
)

const DefaultPosterndPath = "/etc/postern/posternd.toml"

// Duration is a time.Duration that TOML encodes as a string ("90s").
type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d *Duration) UnmarshalTOML(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("duration must be a string like \"90s\", got %T", v)
	}
	return d.UnmarshalText([]byte(s))
}

func (d Duration) MarshalTOML() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

type Posternd struct {
	DBPath                string   `toml:"db_path"`
	SocketPath            string   `toml:"socket_path"`
	TunnelUser            string   `toml:"tunnel_user"`
	AuthorizedKeysPath    string   `toml:"authorized_keys_path"`
	PortMin               int      `toml:"port_min"`
	PortMax               int      `toml:"port_max"`
	HeartbeatOfflineAfter Duration `toml:"heartbeat_offline_after"`
	JoinTokenTTL          Duration `toml:"join_token_ttl"`
	VPSHostname           string   `toml:"vps_hostname"`
}

func DefaultPosternd() Posternd {
	return Posternd{
		DBPath:                "/var/lib/postern/postern.db",
		SocketPath:            "/run/postern/api.sock",
		TunnelUser:            "postern",
		AuthorizedKeysPath:    "/var/lib/postern/authorized_keys",
		PortMin:               2200,
		PortMax:               2299,
		HeartbeatOfflineAfter: Duration(90 * time.Second),
		JoinTokenTTL:          Duration(15 * time.Minute),
		VPSHostname:           "vps.example.net",
	}
}

func LoadPosternd(path string) (Posternd, error) {
	cfg := DefaultPosternd()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Posternd{}, err
	}
	return cfg, nil
}
