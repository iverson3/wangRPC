package wangRPC

import (
	"7go/wangRPC/codec"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
)

// 通信报文格式
// | Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
// | <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|

const MagicNumber = 0x3bef55c

type Option struct {
	MagicNumber int         // MagicNumber marks this's a wangrpc request
	CodecType   codec.Type  // client may choose different Codec to encode body
}

var DefaultOption = &Option{
	MagicNumber: MagicNumber,
	CodecType:   codec.GobType,
}



type Server struct {}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

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
	s.serveCodec(cc)
}


var invalidRequest = struct {}{}

type request struct {
	h            *codec.Header  // header of request
	argv, replyv reflect.Value  // argv and replyv of request
}

func (s *Server) serveCodec(cc codec.Codec) {
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
		go s.handleRequest(cc, req, sending, wg)
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

	// todo: now we don't know the type of request argv
	// now, just support it's string

	req.argv = reflect.New(reflect.TypeOf(""))
	if err = cc.ReadBody(req.argv.Interface()); err != nil {
		log.Println("rpc server: read argv error: ", err)
	}
	return req, nil
}

func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	// 处理请求是并发的，但是回复请求的报文必须是逐个发送的，并发容易导致多个回复报文交织在一起，客户端无法解析
	// 在这里使用锁(sending)保证
	sending.Lock()
	defer sending.Unlock()

	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error: ", err)
	}
}

func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("rpc server: ", req.h, req.argv.Elem())
	req.replyv = reflect.ValueOf(fmt.Sprintf("wangrpc resp %d", req.h.Seq))
	s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
}





























