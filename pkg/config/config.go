package config

import (
	"encoding/xml"
	"fmt"
	"math/rand"
	"os"
	"sync"
)

// DefaultSTUNServers provides modern and stable fallback STUN servers.
var DefaultSTUNServers = []string{
	"stun.voipbuster.com",
	"stun.1und1.de",
	"stun.schlund.de",
	"stun.cloudflare.com",
	"stun.l.google.com:19302",
}

type STUN struct {
	IP string `xml:"ip,attr"`
}

type STUNServer struct {
	STUNs []STUN `xml:"STUN"`
}

type Friend struct {
	Acc  string `xml:"acc,attr"`
	IP   string `xml:"ip,attr"`
	SKey string `xml:"skey,attr"`
	RKey string `xml:"rkey,attr"`
	Name string `xml:"name,attr"`
	Port int    `xml:"port,attr"`
}

type FriendList struct {
	Friends []Friend `xml:"Friend"`
}

type User struct {
	Acc     string `xml:"acc,attr"`
	Name    string `xml:"name,attr"`
	IP      string `xml:"ip,attr"`
	Port    int    `xml:"port,attr"`
	TryPort int    `xml:"tryport,attr"`
	Pwd     string `xml:"pwd,attr"`
}

type Config struct {
	XMLName    xml.Name   `xml:"Config"`
	FriendList FriendList `xml:"FriendList"`
	STUNServer STUNServer `xml:"STUNServer"`
	User       User       `xml:"User"`

	mu sync.RWMutex `xml:"-"`
}

// NewDefaultConfig returns a Config initialized with default STUN servers and random ports.
func NewDefaultConfig() *Config {
	cfg := &Config{}
	for _, s := range DefaultSTUNServers {
		cfg.STUNServer.STUNs = append(cfg.STUNServer.STUNs, STUN{IP: s})
	}
	cfg.User.Port = RandPort()
	cfg.User.TryPort = RandTryPort()
	return cfg
}

func RandPort() int {
	return 50000 + rand.Intn(10000)
}

func RandTryPort() int {
	return 40000 + rand.Intn(10000)
}

// LoadConfig reads the XML configuration from filePath, or initializes defaults if not existing.
func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := NewDefaultConfig()
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", filePath, err)
	}

	cfg := &Config{}
	if err := xml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse xml %s: %w", filePath, err)
	}

	if len(cfg.STUNServer.STUNs) == 0 {
		for _, s := range DefaultSTUNServers {
			cfg.STUNServer.STUNs = append(cfg.STUNServer.STUNs, STUN{IP: s})
		}
	}
	if cfg.User.Port == 0 {
		cfg.User.Port = RandPort()
	}
	if cfg.User.TryPort == 0 {
		cfg.User.TryPort = RandTryPort()
	}

	return cfg, nil
}

// SaveConfig writes the XML configuration out to filePath with formatted indentation.
func (c *Config) SaveConfig(filePath string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := xml.MarshalIndent(c, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Prepend standard XML header if desirable
	out := append([]byte(xml.Header), data...)
	out = append(out, '\n')

	if err := os.WriteFile(filePath, out, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", filePath, err)
	}
	return nil
}

func (c *Config) GetUser() User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.User
}

func (c *Config) SetUser(u User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.User = u
}

func (c *Config) GetFriend(acc string) (Friend, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.FriendList.Friends {
		if f.Acc == acc {
			return f, true
		}
	}
	return Friend{}, false
}

func (c *Config) GetFriendByName(name string) (Friend, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, f := range c.FriendList.Friends {
		if f.Name == name {
			return f, true
		}
	}
	return Friend{}, false
}

func (c *Config) SetFriend(friend Friend) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, f := range c.FriendList.Friends {
		if f.Acc == friend.Acc {
			c.FriendList.Friends[i] = friend
			return
		}
	}
	c.FriendList.Friends = append(c.FriendList.Friends, friend)
}

func (c *Config) SetFriendSKey(acc, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, f := range c.FriendList.Friends {
		if f.Acc == acc {
			c.FriendList.Friends[i].SKey = key
			return
		}
	}
}

func (c *Config) ListFriends() []Friend {
	c.mu.RLock()
	defer c.mu.RUnlock()
	list := make([]Friend, len(c.FriendList.Friends))
	copy(list, c.FriendList.Friends)
	return list
}
