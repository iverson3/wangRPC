package codec

import "io"

// 一个典型的RPC调用如下
// err := client.Call("Arith.Multiply", args, &reply)
// 其中 Arith是服务名
//     Multiply是方法名
//     args是参数
//     reply是返回值
//     error是服务端可能返回的错误信息

type Header struct {
	ServiceMethod string  // format: "Service.Method"
	Seq           uint64  // sequence number chosen by client   请求的序号
	Error         string  // error info from rpc server
}

// 抽象出对消息体进行编解码的接口 Codec
// 抽象出接口是为了实现不同的Codec实例
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}