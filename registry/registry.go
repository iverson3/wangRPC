package registry

import (
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// 注册中心
// 注册中心的好处在于，客户端和服务端都只需要感知注册中心的存在，而无需感知对方的存在

// 1.服务端启动后，向注册中心发送注册消息，注册中心得知该服务已经启动，处于可用状态。一般来说，服务端还需要定期向注册中心发送心跳，证明自己还活着。
// 2.客户端向注册中心询问，当前哪天服务是可用的，注册中心将可用的服务列表返回客户端。
// 3.客户端根据注册中心得到的服务列表，选择其中一个发起调用。

// 常用的注册中心有 etcd、zookeeper、consul
// 一般比较出名的微服务或者 RPC 框架，这些主流的注册中心都是支持的。

type WangRegistry struct {
	timeout time.Duration
	mu      sync.Mutex
	servers map[string]*ServerItem
}

type ServerItem struct {
	Addr  string
	start time.Time
}

const (
	defaultPath = "/_wangrpc_/registry"
	defaultTimeout = time.Minute * 5
)

func New(timeout time.Duration) *WangRegistry {
	return &WangRegistry{
		timeout: timeout,
		servers: make(map[string]*ServerItem),
	}
}

var DefaultWangRegister = New(defaultTimeout)

func (r *WangRegistry) putServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s := r.servers[addr]
	if s == nil {
		r.servers[addr] = &ServerItem{
			Addr:  addr,
			start: time.Now(),
		}
	} else {
		// 如果服务已存在则更新start时间
		s.start = time.Now()
	}
}

func (r *WangRegistry) aliveServers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var alive []string
	for addr, s := range r.servers {
		// 判断server是否处于alive状态
		if r.timeout == 0 || s.start.Add(r.timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			// 已超过timeout则移除该server
			delete(r.servers, addr)
		}
	}
	sort.Strings(alive)
	return alive
}

// 为了实现上的简单，WangRegistry 采用 HTTP 协议提供服务，且所有的有用信息都承载在 HTTP Header 中
func (r *WangRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		w.Header().Set("X-Wangrpc-Servers", strings.Join(r.aliveServers(), ","))
	case "POST":
		addr := req.Header.Get("X-Wangrpc-Server")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.putServer(addr)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *WangRegistry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path: ", registryPath)
}

func HandleHTTP() {
	DefaultWangRegister.HandleHTTP(defaultPath)
}


// 提供Heartbeat方法，便于服务启动时定时向注册中心发送心跳
func Heartbeat(registry, addr string, duration time.Duration) {
	if duration == 0 {
		// 默认周期比注册中心设置的过期时间少 1 min
		duration = defaultTimeout - time.Duration(1) * time.Minute
	}

	var err error
	err = sendHeartbeat(registry, addr)
	go func() {
		// 定时向注册中心发送心跳
		ticker := time.NewTicker(duration)
		for err == nil {
			<-ticker.C
			err = sendHeartbeat(registry, addr)
		}
	}()
}

// 使用HTTP发送心跳数据
func sendHeartbeat(registry, addr string) error {
	log.Println(addr, " send heart beat to registry ", registry)
	req, _ := http.NewRequest("POST", registry, nil)
	req.Header.Set("X-Wangrpc-Server", addr)

	httpClient := &http.Client{}
	_, err := httpClient.Do(req)
	if err != nil {
		log.Println("rpc server: heart beat error: ", err)
		return err
	}
	return nil
}


























