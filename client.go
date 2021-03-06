package wangRPC

import (
	"7go/wangRPC/codec"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ReqNo uint64

type Call struct {
	Seq           ReqNo        // 请求编号
	ServiceMethod string       // format "<service>.<method>"
	Args          interface{}  // arguments to the function
	Reply         interface{}  // reply from the function
	Error         error        // if error occurs, it will be set
	Done          chan *Call   // Strobes when call is complete.
}

// 为了支持异步调用，当调用结束时，会调用call.done()通知调用方
func (call *Call) done() {
	call.Done <- call
}

// 支持异步和并发的高性能客户端
type Client struct {
	cc       codec.Codec    // 消息的编解码器，和服务端类似，用来序列化将要发送出去的请求，以及反序列化接收到的响应
	opt      *Option
	sending  sync.Mutex     // 和服务端类似，保证请求的有序发送，防止出现多个请求报文混淆
	header   codec.Header   // 每个请求的消息头，header 只有在请求发送时才需要，而请求发送是互斥的，因此每个客户端只需要一个，声明在Client结构体中可以复用
	mu       sync.Mutex     // protect following
	seq      ReqNo          // 用于给发送的请求编号，每个请求拥有唯一编号
	pending  map[ReqNo]*Call  // 存储未处理完的请求，键是编号，值是Call实例
	closing  bool   // closing为true 说明是客户端主动关闭的，即调用Close方法
	shutdown bool   // shutdown为true 一般是有错误发生
}

var _ io.Closer = (*Client)(nil)

var ErrShutDown = errors.New("connection is shutdown")

// Close the connection
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing {
		return ErrShutDown
	}
	client.closing = true
	return client.cc.Close()
}

// IsAvailable return true if the client does work
func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()

	// closing 和 shutdown 任意一个值置为true，则表示 Client 处于不可用的状态
	return !client.shutdown && !client.closing
}

func (client *Client) registerCall(call *Call) (ReqNo, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closing || client.shutdown {
		return 0, ErrShutDown
	}

	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *Client) removeCall(seq ReqNo) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()

	call, ok := client.pending[seq]
	if !ok {
		return nil
	}
	delete(client.pending, seq)
	return call
}

// 服务端或客户端发生错误时调用，将shutdown设置为true，并将错误信息通知给所有处于pending状态的call
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()

	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

// Call()是对Go()的一个封装，阻塞在call.Done这等待响应返回，是个同步接口
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

// Go()和Call()是客户端暴露给用户的两个RPC服务调用接口
// Go()是个异步接口
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}

	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}

	client.send(call)
	return call
}

func (client *Client) send(call *Call) {
	// 请求的报文必须是逐个发送的，并发容易导致多个请求报文交织在一起，服务端无法正确解析
	// 在这里使用锁(sending)保证
	client.sending.Lock()
	defer client.sending.Unlock()

	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// 设置请求头header
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq   = uint64(seq)
	client.header.Error = ""

	// encode and send the request
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}

		call := client.removeCall(ReqNo(h.Seq))
		switch {
		case call == nil:
			// 可能是请求没有发送完整，或者因为其他原因被取消，但是服务端仍旧处理了
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			// 服务端处理出错，即 h.Error 不为空
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			// 服务端处理正常，则从body中读取Reply的值
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}

	// error occurs, so terminateCalls pending calls
	client.terminateCalls(err)
}

type clientResult struct {
	client *Client
	err    error
}

type newClientFunc func(conn net.Conn, opt *Option) (client *Client, err error)

func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	// 根据CodecType获取对应的NewCodecFunc函数
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error: ", err)
		return nil, err
	}

	// 先进行协议的交换，即发送 Option 信息给服务端
	// 正如设计的一样，客户端固定采用JSON编码Option，后续的 header 和 body 的编码方式由 Option 中的 CodeType 指定
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}

	// 调用NewCodecFunc函数得到对应的实现了Codec接口的实例
	cc := f(conn)
	return newClientCodec(cc, opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		cc:       cc,
		opt:      opt,
		seq:      1, // seq starts with 1, 0 means invalid call
		pending:  make(map[ReqNo]*Call),
	}
	// 协商好消息的编解码方式之后，再创建一个子协程调用 receive() 接收响应
	go client.receive()
	return client
}

func parseOptions(opts ...*Option) (*Option, error)  {
	if len(opts) == 0 || opts[0] == nil {
		// 使用默认的Option
		return DefaultOption, nil
	}

	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}

	opt := opts[0]
	opt.MagicNumber = DefaultOption.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = DefaultOption.CodecType
	}
	return opt, nil
}

func Dial(network, address string, opts ...*Option) (client *Client, err error) {
	return dialTimeout(NewClient, network, address, opts...)
}

func dialTimeout(f newClientFunc, network, address string, opts ...*Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}

	defer func() {
		if client == nil || err != nil {
			_ = conn.Close()
		}
	}()

	ch := make(chan clientResult)
	// 新建一个协程执行NewClient()，执行完成后则通过chan发送结果
	go func() {
		client2, err2 := f(conn, opt)
		ch <- clientResult{
			client: client2,
			err:    err2,
		}
	}()

	// 没有超时限制
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}

	// 设置超时限制，如果指定的时间内没有从chan中收到NewClient()的结果则当作超时处理
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

// 先用HTTP协议完成了一个握手的过程，然后再使用自己定义的协议来进行双方的通讯
func NewHTTPClient(conn net.Conn, opt *Option) (client *Client, err error) {
	// 发送CONNECT HTTP请求
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))
	// 获取HTTP请求的响应结果
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	// 通过HTTP CONNECT请求建立连接之后，后续的通信过程就交给NewClient()
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

func DialHTTP(network, address string, opts ...*Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

func XDial(rpcAddr string, opts ...*Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client error: wrong format '%s', expect protocol@addr", rpcAddr)
	}

	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		// tcp, unix or other transport protocol
		return Dial(protocol, addr, opts...)
	}
}
















































