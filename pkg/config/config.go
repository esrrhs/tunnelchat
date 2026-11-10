package config

import (
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"tunnelchat/pkg/candidate"
	"tunnelchat/pkg/identity"
)

const CurrentVersion = 2

// ErrLegacyConfig is returned when an old v1 XML config is found.
// The file is left untouched; v2 does not migrate secrets automatically.
var ErrLegacyConfig = errors.New("legacy tunnelchat v1 config detected; v2 is incompatible and will not overwrite it — use a new -c path for a fresh identity")

// ErrExists is returned when creating an identity would overwrite a file.
var ErrExists = errors.New("config already exists; refusing to overwrite identity")

// DefaultSTUNServers are optional public STUN endpoints used only for
// address discovery. They are not chat relays.
var DefaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun.cloudflare.com:3478",
	"stun.1und1.de:3478",
}

type Friend struct {
	Name       string                `json:"name"`
	Ed25519    []byte                `json:"ed25519"`
	X25519     []byte                `json:"x25519"`
	Rendezvous []byte                `json:"rendezvous"`
	Candidates []candidate.Candidate `json:"candidates,omitempty"`
}

func (f Friend) Public() (identity.Public, error) {
	var p identity.Public
	p.Name = f.Name
	if len(f.Ed25519) != 32 || len(f.X25519) != 32 {
		return p, fmt.Errorf("friend %q has invalid public keys", f.Name)
	}
	copy(p.Ed25519[:], f.Ed25519)
	copy(p.X25519[:], f.X25519)
	return p, nil
}

func (f Friend) RendezvousKey() ([32]byte, error) {
	var r [32]byte
	if len(f.Rendezvous) != 32 {
		return r, fmt.Errorf("friend %q has invalid rendezvous secret", f.Name)
	}
	copy(r[:], f.Rendezvous)
	return r, nil
}

type DHTConfig struct {
	// Disabled turns DHT off for this identity (overrides the online
	// default of using public Mainline bootstraps).
	Disabled bool `json:"disabled,omitempty"`
	// Enabled is retained for older v2 files. It is not required to
	// turn DHT on; online now defaults to on unless Disabled is set.
	Enabled   bool     `json:"enabled,omitempty"`
	Bootstrap []string `json:"bootstrap,omitempty"`
}

type RelayConfig struct {
	Enabled     bool   `json:"enabled"`
	Listen      string `json:"listen,omitempty"`
	Server      string `json:"server,omitempty"`
	Token       string `json:"token,omitempty"`
	MaxSessions int    `json:"max_sessions,omitempty"`
}

type User struct {
	Name       string `json:"name"`
	Seed       []byte `json:"seed"`
	Rendezvous []byte `json:"rendezvous"`
}

type Config struct {
	Version     int         `json:"version"`
	User        User        `json:"user"`
	Friends     []Friend    `json:"friends,omitempty"`
	STUNServers []string    `json:"stun_servers,omitempty"`
	ListenPort  int         `json:"listen_port,omitempty"`
	DHT         DHTConfig   `json:"dht"`
	Relay       RelayConfig `json:"relay"`

	path string       `json:"-"`
	mu   sync.RWMutex `json:"-"`
}

func (c *Config) Path() string { return c.path }

func NewEmpty(path string) *Config {
	return &Config{
		Version:     CurrentVersion,
		STUNServers: append([]string(nil), DefaultSTUNServers...),
		path:        path,
	}
}

func (c *Config) Identity() (*identity.Identity, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.User.Seed) != identity.SeedSize {
		return nil, fmt.Errorf("no local identity in %s; run 'tunnelchat new <name>'", c.path)
	}
	return identity.FromSeed(c.User.Name, c.User.Seed)
}

func (c *Config) Rendezvous() ([32]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var r [32]byte
	if len(c.User.Rendezvous) != 32 {
		return r, fmt.Errorf("missing rendezvous secret")
	}
	copy(r[:], c.User.Rendezvous)
	return r, nil
}

// CreateIdentity writes a brand-new account. It never overwrites an
// existing file (O_EXCL) and refuses to run against a legacy XML path.
func CreateIdentity(path, name string) (*Config, error) {
	if err := rejectLegacyPath(path); err != nil {
		return nil, err
	}
	id, err := identity.Generate(name)
	if err != nil {
		return nil, err
	}
	var rv [32]byte
	if _, err := rand.Read(rv[:]); err != nil {
		return nil, err
	}
	cfg := NewEmpty(path)
	cfg.User = User{Name: id.Name, Seed: id.Seed[:], Rendezvous: rv[:]}
	if err := os.MkdirAll(filepath.Dir(absOrCwd(path)), 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Dir may be "." which is fine.
	}
	if err := cfg.saveExclusive(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func absOrCwd(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if dir := filepath.Dir(path); dir != "." {
		return path
	}
	return path
}

func Load(path string) (*Config, error) {
	if err := rejectLegacyPath(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := NewEmpty(path)
			return cfg, nil
		}
		return nil, err
	}
	if looksLegacyXML(data) {
		return nil, fmt.Errorf("%w: %s", ErrLegacyConfig, path)
	}
	cfg := NewEmpty(path)
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Version != 0 && cfg.Version != CurrentVersion {
		return nil, fmt.Errorf("unsupported config version %d in %s", cfg.Version, path)
	}
	cfg.Version = CurrentVersion
	if len(cfg.STUNServers) == 0 {
		cfg.STUNServers = append([]string(nil), DefaultSTUNServers...)
	}
	return cfg, nil
}

func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.saveLocked(false)
}

func (c *Config) saveExclusive() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.saveLocked(true)
}

func (c *Config) saveLocked(exclusive bool) error {
	if c.path == "" {
		return fmt.Errorf("no config path")
	}
	if err := rejectLegacyPath(c.path); err != nil {
		return err
	}
	c.Version = CurrentVersion
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(c.path, flags, 0o600)
	if err != nil {
		if exclusive && os.IsExist(err) {
			return fmt.Errorf("%w: %s", ErrExists, c.path)
		}
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	_ = f.Chmod(0o600)
	return f.Sync()
}

func (c *Config) ListFriends() []Friend {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Friend, len(c.Friends))
	copy(out, c.Friends)
	return out
}

func (c *Config) GetFriendByName(name string) (Friend, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.Friends {
		if strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return Friend{}, false
}

func (c *Config) GetFriendByEd25519(pub []byte) (Friend, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.Friends {
		if len(f.Ed25519) == len(pub) && string(f.Ed25519) == string(pub) {
			return f, true
		}
	}
	return Friend{}, false
}

// AddFriend pins a friend. It refuses to replace an existing name with a
// different identity, or an existing identity with a conflicting name.
func (c *Config) AddFriend(f Friend) error {
	if f.Name == "" || len(f.Ed25519) != 32 || len(f.X25519) != 32 || (len(f.Rendezvous) != 0 && len(f.Rendezvous) != 32) {
		return fmt.Errorf("incomplete friend record")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, existing := range c.Friends {
		sameKey := string(existing.Ed25519) == string(f.Ed25519) && string(existing.X25519) == string(f.X25519)
		sameName := strings.EqualFold(existing.Name, f.Name)
		if sameName && !sameKey {
			return fmt.Errorf("friend %q already exists with a different identity; refusing to overwrite", existing.Name)
		}
		if sameKey {
			// Update hints/name if the identity already exists.
			f.Name = existing.Name
			c.Friends[i] = f
			c.Friends[i].Name = existing.Name
			if f.Name != "" && existing.Name != "" {
				c.Friends[i].Name = existing.Name
			}
			c.Friends[i].Candidates = f.Candidates
			c.Friends[i].Rendezvous = f.Rendezvous
			return nil
		}
	}
	c.Friends = append(c.Friends, f)
	return nil
}

func (c *Config) SetListenPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ListenPort = port
}

func rejectLegacyPath(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".xml") {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: %s", ErrLegacyConfig, path)
		}
		// Even a new .xml path is misleading; refuse so we never write
		// v2 JSON over a name people associate with v1.
		return fmt.Errorf("%w: path %s uses the v1 .xml extension", ErrLegacyConfig, path)
	}
	// If the default v2 path is used, also check a sibling .xml file? We
	// only inspect the given path here; callers may warn separately.
	return nil
}

func looksLegacyXML(data []byte) bool {
	trim := strings.TrimSpace(string(data))
	if !strings.HasPrefix(trim, "<") {
		return false
	}
	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(data, &probe); err != nil {
		return strings.Contains(trim, "<Config") || strings.Contains(trim, "skey=")
	}
	return probe.XMLName.Local == "Config"
}

// HasLegacySibling reports a leftover v1 file next to a v2 path.
func HasLegacySibling(path string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, cand := range []string{base + ".xml", "tunnelchat.xml"} {
		if cand == path {
			continue
		}
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}
