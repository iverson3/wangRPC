package xclient

import (
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"
)

// 服务发现模块

// 不同的负载均衡策略
type SelectMode int

// 定义所有支持的负载均衡策略
const (
	RandomSelect SelectMode = iota  // select randomly
	RoundRobinSelect                // select using Robbin algorithm
)

// 服务发现的接口
type Discovery interface {
	Refresh() error                       // 从注册中心更新服务列表
	Update(servers []string) error        // 手动更新服务列表
	Get(mode SelectMode) (string, error)  // 根据负载均衡策略，选择一个服务实例
	GetAll() ([]string, error)            // 返回所有的服务实例
}

// 实现一个不需要注册中心，服务列表由手工维护的服务发现的结构体
type MultiServersDiscovery struct {
	r       *rand.Rand   // 产生随机数的实例
	mu      sync.RWMutex // protect following
	servers []string
	index   int          // 记录 Round Robin 算法已经轮询到的位置
}

func (d *MultiServersDiscovery) Refresh() error {
	return nil
}

func (d *MultiServersDiscovery) Update(servers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.servers = servers
	return nil
}

func (d *MultiServersDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := len(d.servers)
	if n == 0 {
		return "", errors.New("rpc discovery: no available servers")
	}

	switch mode {
	case RandomSelect:
		return d.servers[d.r.Intn(n)], nil
	case RoundRobinSelect:
		s := d.servers[d.index%n]
		d.index = (d.index + 1) % n
		return s, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (d *MultiServersDiscovery) GetAll() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// return a copy of d.servers
	servers := make([]string, len(d.servers), len(d.servers))
	copy(servers, d.servers)
	return servers, nil
}

var _ Discovery = (*MultiServersDiscovery)(nil)

func NewMultiServerDiscovery(servers []string) *MultiServersDiscovery {
	// 初始化时使用时间戳设定随机数种子，避免每次产生相同的随机数序列
	d := &MultiServersDiscovery{
		r:       rand.New(rand.NewSource(time.Now().UnixNano())),
		servers: servers,
	}
	// 为了避免每次从0开始，初始化时随机设定一个值
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}






























