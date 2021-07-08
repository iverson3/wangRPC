package wangRPC

import (
	"7go/wangRPC/codec"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

// 通信报文格式
// | Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
// | <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|

const MagicNumber = 0x3bef55c

type Option struct {
	MagicNumber    int         // MagicNumber marks this's a wangrpc request
	CodecType      codec.Type  // client may choose different Codec to encode and decode body
	ConnectTimeout time.Duration  // 0 means no limit
	HandleTimeout  time.Duration
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
	ConnectTimeout: time.Second * 10,
}



type Server struct {
	serviceMap sync.Map
}

var DefaultServer = NewServer()

func NewServer() *Server {
	return &Server{}
}

func Register(receiver interface{}) error {
	return DefaultServer.Register(receiver)
}

func (s *Server) Register(receiver interface{}) error {
	s2 := newService(receiver)
	_, dup := s.serviceMap.LoadOrStore(s2.name, s2)
	if dup {
		return errors.New("rpc: service already defined: " + s2.name)
	}
	return nil
}

func (s *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := s.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}

	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}

func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error: ", err)
			return
		}

		go s.ServeConn(conn)
	}
}

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()

	var opt Option

	// 正如设计的一样，服务端首先使用JSON解码Option，然后通过Option的CodeType解码剩余的内容(header和body)
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}

	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x \n", opt.MagicNumber)
		return
	}

	// 根据CodecType从map中获取对应的NewCodecFunc函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s \n", opt.CodecType)
		return
	}
	// 调用NewCodecFunc函数得到对应的实现了Codec接口的实例
	cc := f(conn)
	s.serveCodec(cc, &opt)
}


var invalidRequest = struct {}{}

// request stores all information of a call
type request struct {
	h            *codec.Header  // header of request
	argv, replyv reflect.Value  // argv and replyv of request
	mtype        *methodType
	svc          *service
}

func (s *Server) serveCodec(cc codec.Codec, opt *Option) {
	sending := new(sync.Mutex)  // make sure to send a complete response
	wg      := new(sync.WaitGroup)  // wait until all request are handled

	// 在一次连接中，允许接收多个请求，即多个 request header 和 request body
	// 因此这里使用了for无限制地等待请求的到来，直到发生错误（例如连接被关闭，接收到的报文有问题等）
	for {
		req, err := s.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}

		wg.Add(1)
		go s.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
}

func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error: ", err)
		}
		return nil, err
	}
	return &h, nil
}

func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}

	req := &request{h: h}
	req.svc, req.mtype, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}

	req.argv   = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body error: ", err)
		return req, err
	}

	log.Println("argvi")
	log.Println(argvi)
	//log.Println(req)
	log.Println(req.argv)
	return req, nil
}

func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	// 处理请求是并发的，但是回复请求的报文必须是逐个发送的，并发容易导致多个回复报文交织在一起，客户端无法正确解析
	// 在这里使用锁(sending)保证
	sending.Lock()
	defer sending.Unlock()

	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error: ", err)
	}
}

func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})

	// 这里需要确保 sendResponse()仅调用一次，因此将整个过程拆分为 called 和 sent 两个阶段
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}

	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		s.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

const (
	connected        = "200 Connected to Wang RPC"
	defaultRPCPath   = "/_wangrpc_"       // CONNECT请求的http路径
	defaultDebugPath = "/debug/wangrpc"   // DEBUG页面的地址
)

// 实现Handler接口
// 处理客户端的HTTP CONNECT请求，完成握手过程
// 并取出已建立的HTTP连接中的TCP以进行后续的RPC通信
func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}

	// Hijack()可以将HTTP对应的TCP连接取出 (HTTP通信本就是基于TCP连接的)
	// 连接在Hijack()之后，HTTP的相关操作就会受到影响，调用方需要负责去关闭连接 (不再遵守http协议)
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}

	// 发送HTTP响应数据给客户端
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	// 后续则使用建立的tcp连接正常处理rpc请求
	s.ServeConn(conn)
}

func (s *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, s)

	// 将debugHTTP实例绑定到地址 defaultDebugPath
	http.Handle(defaultDebugPath, debugHTTP{s})
	log.Println("rpc server debug path: ", defaultDebugPath)
}

func HandleHTTP()  {
	DefaultServer.HandleHTTP()
}



// https://liqiang.io/post/hijack-in-go
// https://www.jianshu.com/p/c0c8cec369fd
// https://blog.csdn.net/ya_feng/article/details/104354690
// Hijack()方法，一般在创建连接阶段使用HTTP连接，后续自己完全处理Connection (不再遵守http协议)
// 符合这样的使用场景并不多:
// 1.基于HTTP协议的rpc算一个
// 2.从HTTP升级到WebSocket也算一个


// conn, _, err := w.(http.Hijacker).Hijack()
// 这是一段接管 HTTP 连接的代码
// 所谓的接管HTTP连接是指这里接管了HTTP的TCP连接，也就是说Golang的内置HTTP库和HTTPServer库将不会再管理这个TCP连接的生命周期，其生命周期完全交给Hijacker来管理



















