package main

import (
	"7go/wangRPC"
	"7go/wangRPC/registry"
	"7go/wangRPC/xclient"
	"context"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct {
	Num1 int
	Num2 int
}

// day-4/5/6
func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

// day-6
func (f Foo) Sleep(args Args, reply *int) error {
	time.Sleep(time.Second * time.Duration(args.Num1))
	*reply = args.Num1 + args.Num2
	return nil
}

func main() {
	// day-4
	//log.SetFlags(0)
	//addr := make(chan string)
	//go startServer(addr)
	//
	//client, err := wangRPC.Dial("tcp", <-addr, wangRPC.DefaultOption)
	//if err != nil {
	//	log.Println("rpc client: Dial failed, error: ", err)
	//	return
	//}
	//defer func() { _ = client.Close() }()
	//
	//time.Sleep(time.Second)
	//
	//var wg sync.WaitGroup
	//// send request and receive response
	//for i := 0; i < 5; i++ {
	//	wg.Add(1)
	//	go func(n int) {
	//		defer wg.Done()
	//		args := &Args{
	//			Num1: n,
	//			Num2: n * n,
	//		}
	//		var reply int
	//		err2 := client.Call("", "Foo.Sum", args, &reply)
	//		if err2 != nil {
	//			log.Fatal("call Foo.Sum error: ", err2)
	//		}
	//		log.Printf("reply: %d + %d = %d: ", args.Num1, args.Num2, reply)
	//	}(i)
	//}
	//wg.Wait()


	// day-5
	// 支持HTTP协议之后的通信过程应该是这样的：
	// 1. 客户端向RPC服务器发送HTTP CONNECT请求
	//    CONNECT x.x.x.x:9999/_wangrpc_ HTTP/1.0
	// 2. RPC服务器返回 HTTP 200 状态码表示连接建立
	//    HTTP/1.0 200 Connected to Wang RPC
	// 3. 客户端使用创建好的连接发送RPC报文，先发送Option，再发送 N个请求报文，服务端处理 RPC请求并响应
	//log.SetFlags(0)
	//ch := make(chan string)
	//go callHttp(ch)
	//startServer(ch)


	// day-6
	//log.SetFlags(0)
	//ch1 := make(chan string)
	//ch2 := make(chan string)
	//// start two servers
	//go startServer(ch1)
	//go startServer(ch2)
	//
	//addr1 := <-ch1
	//addr2 := <-ch2
	//
	//time.Sleep(time.Second)
	//call(addr1, addr2)
	//broadcast(addr1, addr2)


	// day-7
	log.SetFlags(0)
	registryAddr := "http://localhost:9999/_wangrpc_/registry"
	var wg sync.WaitGroup
	wg.Add(1)
	go startRegistry(&wg)
	wg.Wait()

	time.Sleep(time.Second)
	wg.Add(2)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()

	time.Sleep(time.Second)
	call(registryAddr)
	broadcast(registryAddr)
}

//func startServer(addrCh chan string) {
	// day-5
	//var foo Foo
	//// pick a free port
	//l, err := net.Listen("tcp", ":9999")
	//if err != nil {
	//	log.Fatal("network error: ", err)
	//}
	//_ = wangRPC.Register(&foo)
	//wangRPC.HandleHTTP()
	//addrCh <- l.Addr().String()
	//_ = http.Serve(l, nil)


	// day-6
	//var foo Foo
	//// pick a free port
	//l, err := net.Listen("tcp", ":0")
	//if err != nil {
	//	log.Fatal("network error: ", err)
	//}
	//server := wangRPC.NewServer()
	//_ = server.Register(&foo)
	//addrCh <- l.Addr().String()
	//server.Accept(l)
//}

// day-7
func startServer(registryAddr string, wg *sync.WaitGroup) {
	var foo Foo
	// pick a free port
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		wg.Done()
		log.Fatal("network error: ", err)
		return
	}
	server := wangRPC.NewServer()
	_ = server.Register(&foo)
	registry.Heartbeat(registryAddr, "tcp@" + l.Addr().String(), 0)
	wg.Done()
	server.Accept(l)
}

// 启动注册中心
func startRegistry(wg *sync.WaitGroup) {
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		wg.Done()
		log.Fatal("network error: ", err)
		return
	}
	registry.HandleHTTP()
	wg.Done()
	_ = http.Serve(l, nil)
}

// day-6
func foo(xc *xclient.XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
	var reply int
	var err error
	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
	}
	if err != nil {
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
	}
}

// day-6
//func call(addr1, addr2 string) {
//	d := xclient.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
//	defer func() { _ = xc.Close() }()
//
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(n int) {
//			defer wg.Done()
//			args := &Args{
//				Num1: n,
//				Num2: n * n,
//			}
//			foo(xc, context.Background(), "call", "Foo.Sum", args)
//		}(i)
//	}
//	wg.Wait()
//}
// day-7
func call(registry string) {
	d := xclient.NewWangRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			args := &Args{
				Num1: n,
				Num2: n * n,
			}
			foo(xc, context.Background(), "call", "Foo.Sum", args)
		}(i)
	}
	wg.Wait()
}

// day-6
//func broadcast(addr1, addr2 string) {
//	d := xclient.NewMultiServerDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
//	defer func() { _ = xc.Close() }()
//
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(n int) {
//			defer wg.Done()
//			args := &Args{
//				Num1: n,
//				Num2: n * n,
//			}
//			foo(xc, context.Background(), "broadcast", "Foo.Sum", args)
//			// expect 2-5 timeout
//			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
//			foo(xc, ctx, "broadcast", "Foo.Sleep", args)
//		}(i)
//	}
//	wg.Wait()
//}
// day-7
func broadcast(registry string) {
	d := xclient.NewWangRegistryDiscovery(registry, 0)
	xc := xclient.NewXClient(d, xclient.RandomSelect, nil)
	defer func() { _ = xc.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			args := &Args{
				Num1: n,
				Num2: n * n,
			}
			foo(xc, context.Background(), "broadcast", "Foo.Sum", args)
			// expect 2-5 timeout
			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
			foo(xc, ctx, "broadcast", "Foo.Sleep", args)
		}(i)
	}
	wg.Wait()
}

// day-5
func callHttp(addrCh chan string)  {
	client, err := wangRPC.DialHTTP("tcp", <-addrCh)
	if err != nil {
		log.Fatal("client: dialHTTP error: ", err)
		return
	}
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			args := &Args{
				Num1: n,
				Num2: n * n,
			}
			var reply int
			err := client.Call(context.Background(), "Foo.Sum", args, &reply)
			if err != nil {
				log.Fatal("call Foo.Sum error: ", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}

























