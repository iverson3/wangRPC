package main

import (
	"7go/wangRPC"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

func main() {
	addr := make(chan string)
	go startServer(addr)

	client, err := wangRPC.Dial("tcp", <-addr, wangRPC.DefaultOption)
	if err != nil {
		log.Println("rpc client: Dial failed, error: ", err)
		return
	}
	defer func() { _ = client.Close() }()

	//conn, err := net.Dial("tcp", <-addr)
	//defer func() { _ = conn.Close() }()
	//if err != nil {
	//	log.Println("rpc client: net.Dial() failed, error: ", err)
	//	return
	//}

	time.Sleep(time.Second)
	// 正如设计的一样，客户端固定采用JSON编码Option，后续的 header 和 body 的编码方式由 Option 中的 CodeType 指定
	//_ = json.NewEncoder(conn).Encode(wangRPC.DefaultOption)

	// 调用NewCodecFunc函数得到对应的实现了Codec接口的实例
	//cc := codec.NewGobCodec(conn)

	var wg sync.WaitGroup
	// send request and receive response
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			args := fmt.Sprintf("wangrpc req %d", n)
			var reply string
			err2 := client.Call("Foo.Sum", args, &reply)
			if err2 != nil {
				log.Fatal("call Foo.Sum error: ", err2)
			}
			log.Println("reply: ", reply)
		}(i)

		//h := &codec.Header{
		//	ServiceMethod: "Foo.Sum",
		//	Seq:           uint64(i),
		//}
		//// 向服务端发送数据
		//_ = cc.Write(h, fmt.Sprintf("wangrpc req %d", h.Seq))
		//
		//// 从服务端接收响应数据 (包括header和body)
		//_ = cc.ReadHeader(h)
		//var reply string
		//_ = cc.ReadBody(&reply)
		//log.Println("reply: ", reply)
	}
	wg.Wait()
}

func startServer(addr chan string) {
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error: ", err)
	}

	log.Println("start rpc server on ", l.Addr())
	addr <- l.Addr().String()
	wangRPC.Accept(l)
}
