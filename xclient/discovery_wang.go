package xclient

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type WangRegistryDiscovery struct {
	*MultiServersDiscovery    // 嵌套了 MultiServersDiscovery，很多能力可以复用
	registry   string         // 注册中心的地址
	timeout    time.Duration  // 服务列表的过期时间
	lastUpdate time.Time      // 最后从注册中心更新服务列表的时间，默认 10s 过期，即 10s 之后，需要从注册中心更新新的列表
}

const defaultUpdateTimeout = time.Second * 10

func NewWangRegistryDiscovery(registerAddr string, timeout time.Duration) *WangRegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	
	return &WangRegistryDiscovery{
		MultiServersDiscovery: NewMultiServerDiscovery(make([]string, 0)),
		registry:              registerAddr,
		timeout:               timeout,
	}
}

func (d *WangRegistryDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.servers = servers
	d.lastUpdate = time.Now()
	return nil
}

// 超时则重新从注册中心获取服务列表
func (d *WangRegistryDiscovery) Refresh() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}

	log.Println("rpc registry: refresh servers from registry ", d.registry)
	resp, err := http.Get(d.registry)
	if err != nil {
		log.Println("rpc registry refresh error: ", err)
		return err
	}

	addrs := resp.Header.Get("X-Wangrpc-Servers")
	servers := strings.Split(addrs, ",")
	d.servers = make([]string, 0, len(servers))
	for _, server := range servers {
		serverAddr := strings.TrimSpace(server)
		if serverAddr != "" {
			d.servers = append(d.servers, serverAddr)
		}
	}
	d.lastUpdate = time.Now()
	return nil
}

func (d *WangRegistryDiscovery) Get(mode SelectMode) (string, error) {
	// 先调用 Refresh 确保服务列表没有过期
	err := d.Refresh()
	if err != nil {
		return "", err
	}
	return d.MultiServersDiscovery.Get(mode)
}

func (d *WangRegistryDiscovery) GetAll() ([]string, error) {
	// 先调用 Refresh 确保服务列表没有过期
	err := d.Refresh()
	if err != nil {
		return nil, err
	}
	return d.MultiServersDiscovery.GetAll()
}

































