package askpass

import "sync"

type PasswordCache struct {
	mu   sync.Mutex
	data map[string]string
}

func NewPasswordCache() *PasswordCache {
	return &PasswordCache{data: make(map[string]string)}
}

func (c *PasswordCache) Get(host string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pw, ok := c.data[host]
	return pw, ok
}

func (c *PasswordCache) Set(host, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[host] = password
}
